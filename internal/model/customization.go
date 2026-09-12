package model

import (
	"errors"
	"time"
)

// MetadataPatch stores explicit local overrides. Nil follows the source;
// pointers to empty/zero values deliberately override it. Refresh leaves it intact.
type MetadataPatch struct {
	Summary       *string   `json:"summary,omitempty"`
	Fandoms       *[]string `json:"fandoms,omitempty"`
	Tags          *[]string `json:"tags,omitempty"`
	Language      *string   `json:"language,omitempty"`
	Rating        *string   `json:"rating,omitempty"`
	Words         *int      `json:"words,omitempty"`
	Chapters      *int      `json:"chapters,omitempty"`
	TotalChapters *int      `json:"total_chapters,omitempty"`
	Complete      *bool     `json:"complete,omitempty"`
	Published     *string   `json:"published,omitempty"`
	Updated       *string   `json:"updated,omitempty"`
}

func (p MetadataPatch) Validate() error {
	for _, n := range []*int{p.Words, p.Chapters, p.TotalChapters} {
		if n != nil && *n < 0 {
			return errors.New("word and chapter counts must be zero or greater")
		}
	}
	for _, d := range []*string{p.Published, p.Updated} {
		if d != nil && *d != "" {
			if _, err := time.Parse("2006-01-02", *d); err != nil {
				return errors.New("source dates must be YYYY-MM-DD or empty")
			}
		}
	}
	return nil
}

// Merge changes only supplied override fields.
func (p *MetadataPatch) Merge(v MetadataPatch) {
	if v.Summary != nil {
		p.Summary = v.Summary
	}
	if v.Fandoms != nil {
		cleaned := CleanTags(*v.Fandoms)
		p.Fandoms = &cleaned
	}
	if v.Tags != nil {
		cleaned := CleanTags(*v.Tags)
		p.Tags = &cleaned
	}
	if v.Language != nil {
		p.Language = v.Language
	}
	if v.Rating != nil {
		p.Rating = v.Rating
	}
	if v.Words != nil {
		p.Words = v.Words
	}
	if v.Chapters != nil {
		p.Chapters = v.Chapters
	}
	if v.TotalChapters != nil {
		p.TotalChapters = v.TotalChapters
	}
	if v.Complete != nil {
		p.Complete = v.Complete
	}
	if v.Published != nil {
		p.Published = v.Published
	}
	if v.Updated != nil {
		p.Updated = v.Updated
	}
}

// EffectiveMetadata is the source snapshot overlaid with personal customization.
func (b Bookmark) EffectiveMetadata() Metadata {
	var m Metadata
	if b.Metadata != nil {
		m = *b.Metadata
	}
	p := b.Overrides
	if p == nil {
		return m
	}
	if p.Summary != nil {
		m.Summary = *p.Summary
	}
	if p.Fandoms != nil {
		m.Fandoms = *p.Fandoms
	}
	if p.Tags != nil {
		m.Tags = *p.Tags
	}
	if p.Language != nil {
		m.Language = *p.Language
	}
	if p.Rating != nil {
		m.Rating = *p.Rating
	}
	if p.Words != nil {
		m.Words = *p.Words
	}
	if p.Chapters != nil {
		m.Chapters = *p.Chapters
	}
	if p.TotalChapters != nil {
		m.TotalChapters = *p.TotalChapters
	}
	if p.Complete != nil {
		m.Complete = *p.Complete
	}
	if p.Published != nil {
		m.Published = *p.Published
	}
	if p.Updated != nil {
		m.Updated = *p.Updated
	}
	return m
}

type Progress struct {
	Read      int  `json:"read"`
	Published int  `json:"published"`
	Unread    int  `json:"unread"`
	Known     bool `json:"known"`
	Percent   int  `json:"percent"`
}

// ReadingProgress uses published chapters, not the author's planned total.
func (b Bookmark) ReadingProgress() Progress {
	n := b.EffectiveMetadata().Chapters
	p := Progress{Read: b.Chapter, Published: n, Known: n > 0}
	if p.Known {
		p.Unread = max(0, n-b.Chapter)
		p.Percent = int(min(100, float64(b.Chapter)/float64(n)*100))
	}
	return p
}
