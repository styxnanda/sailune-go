package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/styxnanda/sailune-go/internal/model"
)

var ErrNotFound = errors.New("bookmark not found")
var ErrDuplicate = errors.New("story already bookmarked")

type Library struct{ Store Store }

// MetadataFetcher lets other frontends and future site adapters share this flow.
type MetadataFetcher interface {
	Fetch(context.Context, string) (Metadata, error)
}

// AddScraped validates and checks duplicates before fetching. Failed fetches
// never create bookmarks; Add rechecks duplicates under the write lock.
func (l Library) AddScraped(ctx context.Context, b Bookmark, fetcher MetadataFetcher) (Bookmark, error) {
	canonical, _, _, err := model.NormalizeURL(b.URL)
	if err != nil {
		return Bookmark{}, err
	}
	if b.Status == "" {
		b.Status = Planned
	}
	if err := model.Validate(b); err != nil {
		return Bookmark{}, err
	}
	existingID, err := l.Store.findURL(canonical)
	if err != nil {
		return Bookmark{}, err
	}
	if existingID != 0 {
		return Bookmark{}, fmt.Errorf("%w (ID %d)", ErrDuplicate, existingID)
	}
	if fetcher == nil {
		return Bookmark{}, errors.New("metadata fetcher is required")
	}
	m, err := fetcher.Fetch(ctx, canonical)
	if err != nil {
		return Bookmark{}, err
	}
	b.Metadata = &m
	if strings.TrimSpace(b.Title) == "" {
		b.Title = m.Title
	}
	if strings.TrimSpace(b.Author) == "" {
		b.Author = strings.Join(m.Authors, ", ")
	}
	return l.Add(b)
}

// Add uses URL and personal metadata; identity and timestamps are assigned here.
func (l Library) Add(b Bookmark) (Bookmark, error) {
	var err error
	b.URL, b.Site, b.WorkID, err = model.NormalizeURL(b.URL)
	if err != nil {
		return Bookmark{}, err
	}
	if b.Status == "" {
		b.Status = Planned
	}
	b.Title, b.Author = strings.TrimSpace(b.Title), strings.TrimSpace(b.Author)
	b.Tags = model.CleanTags(b.Tags)
	if err := model.Validate(b); err != nil {
		return Bookmark{}, err
	}
	err = l.Store.write(func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRow("SELECT id FROM bookmarks WHERE url=?", b.URL).Scan(&id)
		if err == nil {
			return fmt.Errorf("%w (ID %d)", ErrDuplicate, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow("SELECT next_id FROM library_meta WHERE singleton=1").Scan(&b.ID); err != nil {
			return err
		}
		if b.ID == math.MaxInt64 {
			return errors.New("library ID limit reached")
		}
		if _, err := tx.Exec("UPDATE library_meta SET next_id=next_id+1 WHERE singleton=1"); err != nil {
			return err
		}
		b.CreatedAt = time.Now().UTC()
		b.UpdatedAt = b.CreatedAt
		if b.Chapter > 0 && b.LastReadAt.IsZero() {
			b.LastReadAt = b.CreatedAt
		}
		return saveBookmark(tx, b)
	})
	return b, err
}

func (l Library) Get(id int64) (Bookmark, error) {
	db, err := l.Store.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return Bookmark{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	if err != nil {
		return Bookmark{}, err
	}
	defer db.Close()
	b, err := scanBookmark(db.QueryRow("SELECT payload FROM bookmarks WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Bookmark{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	return b, err
}

// Refresh fetches a new source snapshot without changing personal fields.
// Fetching happens outside the write lock; concurrent edits are preserved.
func (l Library) Refresh(ctx context.Context, id int64, fetcher MetadataFetcher) (Bookmark, error) {
	b, err := l.Get(id)
	if err != nil {
		return Bookmark{}, err
	}
	if fetcher == nil {
		return Bookmark{}, errors.New("metadata fetcher is required")
	}
	if err := ctx.Err(); err != nil {
		return Bookmark{}, err
	}
	m, err := fetcher.Fetch(ctx, b.URL)
	if err != nil {
		return Bookmark{}, err
	}
	return l.Store.mutate(id, func(current *Bookmark) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current.Metadata = &m
		return nil
	})
}

// Patch uses pointers so an omitted field differs from an explicit empty value.
type Patch struct {
	Title          *string
	Author         *string
	Status         *Status
	Chapter        *int
	Tags           *[]string
	Notes          *string
	Rating         *int
	ReviewNotes    *string
	LastReadAt     *time.Time
	CreatedAt      *time.Time
	Overrides      *model.MetadataPatch
	ResetOverrides *string // comma-separated override names; "all" clears all
}

func (l Library) Update(id int64, p Patch) (Bookmark, error) {
	return l.Store.mutate(id, func(b *Bookmark) error {
		if p.Title != nil {
			b.Title = strings.TrimSpace(*p.Title)
		}
		if p.Author != nil {
			b.Author = strings.TrimSpace(*p.Author)
		}
		if p.Status != nil {
			b.Status = *p.Status
		}
		if p.Chapter != nil {
			b.Chapter = *p.Chapter
			if b.Chapter > 0 {
				b.LastReadAt = time.Now().UTC()
			} else {
				b.LastReadAt = time.Time{}
			}
		}
		if p.Tags != nil {
			b.Tags = model.CleanTags(*p.Tags)
		}
		if p.Notes != nil {
			b.Notes = *p.Notes
		}
		if p.Rating != nil {
			b.Rating = *p.Rating
		}
		if p.ReviewNotes != nil {
			b.ReviewNotes = *p.ReviewNotes
		}
		if p.LastReadAt != nil {
			b.LastReadAt = p.LastReadAt.UTC()
		}
		if p.CreatedAt != nil {
			if p.CreatedAt.IsZero() {
				return errors.New("added date cannot be empty")
			}
			b.CreatedAt = p.CreatedAt.UTC()
		}
		if p.ResetOverrides != nil {
			if err := resetOverrides(b, *p.ResetOverrides); err != nil {
				return err
			}
		}
		if p.Overrides != nil {
			if b.Overrides == nil {
				b.Overrides = &model.MetadataPatch{}
			}
			b.Overrides.Merge(*p.Overrides)
		}
		return nil
	})
}

func (l Library) Delete(id int64) error {
	return l.Store.write(func(tx *sql.Tx) error {
		result, err := tx.Exec("DELETE FROM bookmarks WHERE id=?", id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: %d", ErrNotFound, id)
		}
		return nil
	})
}

func (s Store) findURL(url string) (int64, error) {
	db, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var id int64
	err = db.QueryRow("SELECT id FROM bookmarks WHERE url=?", url).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
