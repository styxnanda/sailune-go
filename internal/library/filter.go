package library

import (
	"cmp"
	"errors"
	"sort"
	"strings"
)

type Filter struct {
	Query     string
	Site      Site
	Status    Status
	Tag       string
	Author    string
	Fandom    string
	Language  string
	SourceTag string
	Complete  *bool
	Unread    bool
	MinRating int
	MinWords  int
	MaxWords  int    // zero means no upper bound
	Sort      string // added (default), last-read, updated, source-updated, title, author, rating, words, progress
	Desc      bool
	Limit     int // zero means unlimited
	Offset    int
}

func contains(value, query string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(query))
}
func has(values []string, value string) bool {
	for _, v := range values {
		if strings.EqualFold(v, value) {
			return true
		}
	}
	return false
}

// List searches effective metadata and personal fields; all filters combine with AND.
// Query words may match different fields. Sorting is deterministic, with ID ties.
func (l Library) List(f Filter) ([]Bookmark, error) {
	if f.Site != "" && f.Site != AO3 && f.Site != FFN {
		return nil, errors.New("site must be ao3 or ffn")
	}
	if f.Status != "" && !f.Status.Valid() {
		return nil, errors.New("invalid status: use planned, reading, completed, hold, or dropped")
	}
	if f.MinRating < 0 || f.MinRating > 5 {
		return nil, errors.New("min-rating must be 0–5")
	}
	if f.MinWords < 0 || f.MaxWords < 0 || f.MaxWords > 0 && f.MaxWords < f.MinWords {
		return nil, errors.New("invalid word range")
	}
	if f.Limit < 0 || f.Offset < 0 {
		return nil, errors.New("limit and offset must be zero or greater")
	}
	switch f.Sort {
	case "", "added", "last-read", "updated", "source-updated", "title", "author", "rating", "words", "progress":
	default:
		return nil, errors.New("sort must be added, last-read, updated, source-updated, title, author, rating, words, or progress")
	}
	db, err := l.Store.read()
	if err != nil {
		return nil, err
	}
	result := []Bookmark{}
	for _, b := range db.Bookmarks {
		m := b.EffectiveMetadata()
		if f.Site != "" && b.Site != f.Site || f.Status != "" && b.Status != f.Status {
			continue
		}
		if f.Tag != "" && !has(b.Tags, f.Tag) || f.SourceTag != "" && !has(m.Tags, f.SourceTag) {
			continue
		}
		if !contains(b.Author+"\n"+strings.Join(m.Authors, "\n"), f.Author) {
			continue
		}
		if f.Fandom != "" && !has(m.Fandoms, f.Fandom) || f.Language != "" && !strings.EqualFold(m.Language, f.Language) {
			continue
		}
		if f.Complete != nil && (b.Metadata == nil && (b.Overrides == nil || b.Overrides.Complete == nil) || m.Complete != *f.Complete) {
			continue
		}
		if f.Unread && b.ReadingProgress().Unread == 0 || b.Rating < f.MinRating {
			continue
		}
		if m.Words < f.MinWords || f.MaxWords > 0 && m.Words > f.MaxWords {
			continue
		}
		haystack := strings.Join([]string{b.Title, b.Author, b.URL, b.Notes, b.ReviewNotes, strings.Join(b.Tags, " "), m.Title, strings.Join(m.Authors, " "), m.Summary, strings.Join(m.Fandoms, " "), strings.Join(m.Tags, " "), m.Language, m.Rating}, "\n")
		matches := true
		for _, term := range strings.Fields(f.Query) {
			if !contains(haystack, term) {
				matches = false
				break
			}
		}
		if matches {
			result = append(result, b)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		am, bm := a.EffectiveMetadata(), b.EffectiveMetadata()
		order := 0
		switch f.Sort {
		case "", "added":
			order = a.CreatedAt.Compare(b.CreatedAt)
		case "last-read":
			order = a.LastReadAt.Compare(b.LastReadAt)
		case "updated":
			order = a.UpdatedAt.Compare(b.UpdatedAt)
		case "source-updated":
			order = cmp.Compare(am.Updated, bm.Updated)
		case "title":
			order = cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		case "author":
			order = cmp.Compare(strings.ToLower(a.Author), strings.ToLower(b.Author))
		case "rating":
			order = cmp.Compare(a.Rating, b.Rating)
		case "words":
			order = cmp.Compare(am.Words, bm.Words)
		case "progress":
			order = cmp.Compare(a.ReadingProgress().Percent, b.ReadingProgress().Percent)
		}
		if order == 0 {
			return a.ID < b.ID
		}
		if f.Desc {
			return order > 0
		}
		return order < 0
	})
	if f.Offset >= len(result) {
		return []Bookmark{}, nil
	}
	result = result[f.Offset:]
	if f.Limit > 0 && f.Limit < len(result) {
		result = result[:f.Limit]
	}
	return result, nil
}

func resetOverrides(b *Bookmark, names string) error {
	if b.Overrides == nil {
		b.Overrides = &MetadataPatch{}
	}
	p := b.Overrides
	for _, name := range strings.Split(names, ",") {
		switch strings.TrimSpace(name) {
		case "all":
			*p = MetadataPatch{}
		case "summary":
			p.Summary = nil
		case "fandoms":
			p.Fandoms = nil
		case "source-tags":
			p.Tags = nil
		case "language":
			p.Language = nil
		case "content-rating":
			p.Rating = nil
		case "words":
			p.Words = nil
		case "chapters":
			p.Chapters = nil
		case "total-chapters":
			p.TotalChapters = nil
		case "complete":
			p.Complete = nil
		case "published":
			p.Published = nil
		case "source-updated":
			p.Updated = nil
		default:
			return errors.New("unknown override field: " + name)
		}
	}
	return nil
}
