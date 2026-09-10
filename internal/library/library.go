package library

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	existing, err := l.List(Filter{})
	if err != nil {
		return Bookmark{}, err
	}
	for _, saved := range existing {
		if saved.URL == canonical {
			return Bookmark{}, fmt.Errorf("%w (ID %d)", ErrDuplicate, saved.ID)
		}
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
	err = l.Store.change(func(db *database) error {
		for _, existing := range db.Bookmarks {
			if existing.URL == b.URL {
				return fmt.Errorf("%w (ID %d)", ErrDuplicate, existing.ID)
			}
		}
		if db.NextID == math.MaxInt64 {
			return errors.New("library ID limit reached")
		}
		b.ID = db.NextID
		db.NextID++
		b.CreatedAt = time.Now().UTC()
		b.UpdatedAt = b.CreatedAt
		db.Bookmarks = append(db.Bookmarks, b)
		return nil
	})
	return b, err
}

type Filter struct {
	Query  string
	Site   Site
	Status Status
	Tag    string
}

// List returns matches in creation order. Query is case-insensitive substring search.
func (l Library) List(f Filter) ([]Bookmark, error) {
	if f.Site != "" && f.Site != AO3 && f.Site != FFN {
		return nil, errors.New("site must be ao3 or ffn")
	}
	if f.Status != "" && !f.Status.Valid() {
		return nil, errors.New("invalid status: use planned, reading, completed, hold, or dropped")
	}
	db, err := l.Store.read()
	if err != nil {
		return nil, err
	}
	result := []Bookmark{}
	for _, b := range db.Bookmarks {
		if f.Site != "" && b.Site != f.Site || f.Status != "" && b.Status != f.Status {
			continue
		}
		if f.Tag != "" {
			found := false
			for _, tag := range b.Tags {
				if strings.EqualFold(tag, f.Tag) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		haystack := strings.Join([]string{b.Title, b.Author, b.URL, b.Notes, strings.Join(b.Tags, " ")}, "\n")
		if b.Metadata != nil {
			haystack += "\n" + strings.Join([]string{b.Metadata.Title, strings.Join(b.Metadata.Authors, " "), b.Metadata.Summary, strings.Join(b.Metadata.Fandoms, " "), strings.Join(b.Metadata.Tags, " ")}, "\n")
		}
		if !strings.Contains(strings.ToLower(haystack), strings.ToLower(f.Query)) {
			continue
		}
		result = append(result, b)
	}
	return result, nil
}

func (l Library) Get(id int64) (Bookmark, error) {
	db, err := l.Store.read()
	if err != nil {
		return Bookmark{}, err
	}
	for _, b := range db.Bookmarks {
		if b.ID == id {
			return b, nil
		}
	}
	return Bookmark{}, fmt.Errorf("%w: %d", ErrNotFound, id)
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
	var result Bookmark
	err = l.Store.change(func(db *database) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, current := range db.Bookmarks {
			if current.ID != id {
				continue
			}
			current.Metadata = &m
			current.UpdatedAt = time.Now().UTC()
			db.Bookmarks[i], result = current, current
			return nil
		}
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	})
	return result, err
}

// Patch uses pointers so an omitted field differs from an explicit empty value.
type Patch struct {
	Title   *string
	Author  *string
	Status  *Status
	Chapter *int
	Tags    *[]string
	Notes   *string
}

func (l Library) Update(id int64, p Patch) (Bookmark, error) {
	var result Bookmark
	err := l.Store.change(func(db *database) error {
		for i, b := range db.Bookmarks {
			if b.ID != id {
				continue
			}
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
			}
			if p.Tags != nil {
				b.Tags = model.CleanTags(*p.Tags)
			}
			if p.Notes != nil {
				b.Notes = *p.Notes
			}
			if err := model.Validate(b); err != nil {
				return err
			}
			b.UpdatedAt = time.Now().UTC()
			db.Bookmarks[i], result = b, b
			return nil
		}
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	})
	return result, err
}

func (l Library) Delete(id int64) error {
	return l.Store.change(func(db *database) error {
		for i, b := range db.Bookmarks {
			if b.ID == id {
				db.Bookmarks = append(db.Bookmarks[:i], db.Bookmarks[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	})
}
