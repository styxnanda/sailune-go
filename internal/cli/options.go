package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	sailune "github.com/styxnanda/sailune-go"
)

type customizationOptions struct {
	rating                                                                          int
	review, lastRead, added, reset                                                  string
	summary, fandoms, sourceTags, language, contentRating, published, sourceUpdated string
	words, chapters, total                                                          int
	complete                                                                        bool
}

func (o *customizationOptions) register(fs *flag.FlagSet) {
	fs.IntVar(&o.rating, "rating", 0, "personal stars: 1–5; 0 clears rating")
	fs.StringVar(&o.review, "review-notes", "", "personal review, separate from notes; empty clears")
	fs.StringVar(&o.lastRead, "last-read", "", "last read: RFC3339, YYYY-MM-DD, now, or empty to clear")
	fs.StringVar(&o.added, "added", "", "date added: RFC3339 or YYYY-MM-DD")
	fs.StringVar(&o.reset, "reset-overrides", "", "comma-separated metadata fields to follow source again, or all")
	fs.StringVar(&o.summary, "summary", "", "custom summary; empty clears")
	fs.StringVar(&o.fandoms, "fandoms", "", "custom comma-separated fandoms")
	fs.StringVar(&o.sourceTags, "source-tags", "", "custom comma-separated source tags")
	fs.StringVar(&o.language, "language", "", "custom language")
	fs.StringVar(&o.contentRating, "content-rating", "", "custom source content rating, separate from personal stars")
	fs.StringVar(&o.published, "published", "", "custom publication date: YYYY-MM-DD or empty")
	fs.StringVar(&o.sourceUpdated, "source-updated", "", "custom source update date: YYYY-MM-DD or empty")
	fs.IntVar(&o.words, "words", 0, "custom word count")
	fs.IntVar(&o.chapters, "chapters", 0, "custom published chapter count (0 = unknown)")
	fs.IntVar(&o.total, "total-chapters", 0, "custom planned chapter count (0 = unknown)")
	fs.BoolVar(&o.complete, "complete", false, "custom story publication completion; use --complete=false to unset")
}

func parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if value == "now" {
		return time.Now().UTC(), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("date must be RFC3339, YYYY-MM-DD, now, or empty")
}

func (o *customizationOptions) patch(fs *flag.FlagSet, p *sailune.Patch) error {
	var err error
	m := sailune.MetadataPatch{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "rating":
			p.Rating = &o.rating
		case "review-notes":
			p.ReviewNotes = &o.review
		case "last-read", "added":
			value := o.lastRead
			if f.Name == "added" {
				value = o.added
			}
			date, e := parseDate(value)
			if e != nil {
				err = fmt.Errorf("%s: %w", f.Name, e)
				return
			}
			if f.Name == "added" {
				p.CreatedAt = &date
			} else {
				p.LastReadAt = &date
			}
		case "reset-overrides":
			p.ResetOverrides = &o.reset
		case "summary":
			m.Summary = &o.summary
		case "fandoms":
			v := strings.Split(o.fandoms, ",")
			m.Fandoms = &v
		case "source-tags":
			v := strings.Split(o.sourceTags, ",")
			m.Tags = &v
		case "language":
			m.Language = &o.language
		case "content-rating":
			m.Rating = &o.contentRating
		case "published":
			m.Published = &o.published
		case "source-updated":
			m.Updated = &o.sourceUpdated
		case "words":
			m.Words = &o.words
		case "chapters":
			m.Chapters = &o.chapters
		case "total-chapters":
			m.TotalChapters = &o.total
		case "complete":
			m.Complete = &o.complete
		}
	})
	if m != (sailune.MetadataPatch{}) {
		p.Overrides = &m
	}
	return err
}

type listOptions struct {
	filter   sailune.Filter
	complete bool
}

func (o *listOptions) register(fs *flag.FlagSet) {
	f := &o.filter
	fs.StringVar(&f.Collection, "collection", "", "collection ID")
	fs.StringVar(&f.Author, "author", "", "author contains text, ignoring case")
	fs.StringVar(&f.Fandom, "fandom", "", "exact fandom, ignoring case")
	fs.StringVar(&f.Language, "language", "", "exact language, ignoring case")
	fs.StringVar(&f.SourceTag, "source-tag", "", "exact source tag, ignoring case")
	fs.BoolVar(&o.complete, "complete", false, "filter publication completion; use --complete=false for ongoing")
	fs.BoolVar(&f.Unread, "unread", false, "only stories with known unread published chapters")
	fs.IntVar(&f.MinRating, "min-rating", 0, "minimum personal stars, 0–5")
	fs.IntVar(&f.MinWords, "min-words", 0, "minimum word count")
	fs.IntVar(&f.MaxWords, "max-words", 0, "maximum word count; 0 = unlimited")
	fs.StringVar(&f.Sort, "sort", "added", "added, last-read, updated, source-updated, title, author, rating, words, progress")
	fs.BoolVar(&f.Desc, "desc", false, "sort descending")
	fs.IntVar(&f.Limit, "limit", 0, "maximum results; 0 = unlimited")
	fs.IntVar(&f.Offset, "offset", 0, "skip this many sorted matches")
}

func progressText(b sailune.Bookmark) string {
	p := b.ReadingProgress()
	if !p.Known {
		return fmt.Sprintf("%d/?", p.Read)
	}
	return fmt.Sprintf("%d/%d (%d%%; %d unread)", p.Read, p.Published, p.Percent, p.Unread)
}
func dateText(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}
func ratingText(r int) string {
	if r == 0 {
		return "unrated"
	}
	return fmt.Sprintf("%d/5", r)
}

type bookmarkOutput struct {
	sailune.Bookmark
	EffectiveMetadata sailune.Metadata `json:"effective_metadata"`
	Progress          sailune.Progress `json:"progress"`
}

func outputBookmark(b sailune.Bookmark) bookmarkOutput {
	return bookmarkOutput{b, b.EffectiveMetadata(), b.ReadingProgress()}
}
