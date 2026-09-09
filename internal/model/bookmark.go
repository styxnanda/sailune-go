// Package model defines bookmarks, source metadata, and supported site identities.
package model

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Site string

const (
	AO3 Site = "ao3"
	FFN Site = "ffn"
)

type Status string

const (
	Planned   Status = "planned"
	Reading   Status = "reading"
	Completed Status = "completed"
	OnHold    Status = "hold"
	Dropped   Status = "dropped"
)

func (s Status) Valid() bool {
	switch s {
	case Planned, Reading, Completed, OnHold, Dropped:
		return true
	}
	return false
}

// Bookmark separates personal reading progress from the site's work identity.
type Bookmark struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Site      Site      `json:"site"`
	WorkID    string    `json:"work_id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Status    Status    `json:"status"`
	Chapter   int       `json:"chapter"`
	Tags      []string  `json:"tags"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Metadata  *Metadata `json:"metadata,omitempty"`
}

// Metadata is a snapshot from the source site, separate from personal fields.
// TotalChapters is zero when the author has not declared a planned total.
type Metadata struct {
	Title         string    `json:"title"`
	Authors       []string  `json:"authors"`
	Summary       string    `json:"summary"`
	Fandoms       []string  `json:"fandoms"`
	Tags          []string  `json:"tags"`
	Language      string    `json:"language"`
	Rating        string    `json:"rating"`
	Words         int       `json:"words"`
	Chapters      int       `json:"chapters"`
	TotalChapters int       `json:"total_chapters"`
	Complete      bool      `json:"complete"`
	Published     string    `json:"published,omitempty"`
	Updated       string    `json:"updated,omitempty"`
	FetchedAt     time.Time `json:"fetched_at"`
}

var ao3Path = regexp.MustCompile(`^/works/([0-9]+)(?:/chapters/[0-9]+)?/?$`)
var ffnPath = regexp.MustCompile(`^/s/([0-9]+)(?:/[1-9][0-9]*(?:/[^/]+)?)?/?$`)

// NormalizeURL accepts work/chapter URLs and returns a canonical work identity.
// Query strings, fragments, chapter numbers, and FFN slugs are not identity.
func NormalizeURL(raw string) (canonical string, site Site, workID string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" {
		return "", "", "", errors.New("expected an HTTP(S) AO3 or FanFiction.net story URL")
	}
	var match []string
	switch strings.ToLower(u.Host) {
	case "archiveofourown.org", "www.archiveofourown.org":
		site, match = AO3, ao3Path.FindStringSubmatch(u.EscapedPath())
	case "fanfiction.net", "www.fanfiction.net", "m.fanfiction.net":
		site, match = FFN, ffnPath.FindStringSubmatch(u.EscapedPath())
	default:
		return "", "", "", errors.New("unsupported site: MVP supports archiveofourown.org and fanfiction.net")
	}
	if match == nil || strings.TrimLeft(match[1], "0") == "" {
		return "", "", "", errors.New("expected a story URL: AO3 /works/123 or FFN /s/123/1")
	}
	workID = strings.TrimLeft(match[1], "0")
	if site == AO3 {
		canonical = "https://archiveofourown.org/works/" + workID
	} else {
		canonical = "https://www.fanfiction.net/s/" + workID + "/1"
	}
	return canonical, site, workID, nil
}

func CleanTags(tags []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag != "" && !seen[key] {
			result = append(result, tag)
			seen[key] = true
		}
	}
	return result
}

func Validate(b Bookmark) error {
	if !b.Status.Valid() {
		return fmt.Errorf("invalid status %q: use planned, reading, completed, hold, or dropped", b.Status)
	}
	if b.Chapter < 0 {
		return errors.New("chapter must be zero or greater")
	}
	return nil
}
