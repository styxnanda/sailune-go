package library

import (
	"errors"
	"os"
	"strings"
)

type Filter struct {
	count      *int
	Collection string
	Rules      *CollectionRules
	Query      string
	Site       Site
	Status     Status
	Tag        string
	Author     string
	Fandom     string
	Language   string
	SourceTag  string
	Complete   *bool
	Unread     bool
	MinRating  int
	MinWords   int
	MaxWords   int    // zero means no upper bound
	Sort       string // added (default), last-read, updated, source-updated, title, author, rating, words, progress
	Desc       bool
	Limit      int // zero means unlimited
	Offset     int
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
	db, err := l.Store.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return []Bookmark{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	clauses := []string{"1=1"}
	args := []any{}
	add := func(clause string, values ...any) { clauses = append(clauses, clause); args = append(args, values...) }
	if f.Collection != "" {
		c, e := readCollection(db, f.Collection)
		if e != nil {
			return nil, e
		}
		clause, values, e := collectionClause(c)
		if e != nil {
			return nil, e
		}
		add("("+clause+")", values...)
	}
	if f.Rules != nil {
		clause, values, e := ruleClause(*f.Rules)
		if e != nil {
			return nil, e
		}
		add("("+clause+")", values...)
	}
	if f.Site != "" {
		add("site=?", f.Site)
	}
	if f.Status != "" {
		add("status=?", f.Status)
	}
	if f.Language != "" {
		add("language=?", fold(f.Language))
	}
	if f.Author != "" {
		add("instr(author_search,?)>0", strings.ToLower(f.Author))
	}
	for kind, value := range map[string]string{"tag": f.Tag, "source-tag": f.SourceTag, "fandom": f.Fandom} {
		if value != "" {
			add("id IN (SELECT bookmark_id FROM facets WHERE kind=? AND value=?)", kind, fold(value))
		}
	}
	if f.Complete != nil {
		add("complete=?", *f.Complete)
	}
	if f.Unread {
		add("unread>0")
	}
	if f.MinRating > 0 {
		add("rating>=?", f.MinRating)
	}
	if f.MinWords > 0 {
		add("words>=?", f.MinWords)
	}
	if f.MaxWords > 0 {
		add("words<=?", f.MaxWords)
	}
	for _, term := range strings.Fields(f.Query) {
		add("instr(search_text,?)>0", strings.ToLower(term))
	}
	if f.count != nil {
		err := db.QueryRow("SELECT count(*) FROM bookmarks WHERE "+strings.Join(clauses, " AND "), args...).Scan(f.count)
		return nil, err
	}
	columns := map[string]string{"": "added", "added": "added", "last-read": "last_read", "updated": "updated", "source-updated": "source_updated", "title": "title", "author": "author", "rating": "rating", "words": "words", "progress": "progress"}
	direction := "ASC"
	if f.Desc {
		direction = "DESC"
	}
	query := "SELECT payload FROM bookmarks WHERE " + strings.Join(clauses, " AND ") + " ORDER BY " + columns[f.Sort] + " " + direction + ",id ASC LIMIT ? OFFSET ?"
	limit := f.Limit
	if limit == 0 {
		limit = -1
	}
	args = append(args, limit, f.Offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Bookmark{}
	for rows.Next() {
		b, err := scanBookmark(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
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

func (l Library) Count(f Filter) (int, error) {
	n := 0
	f.count = &n
	_, err := l.List(f)
	return n, err
}
