package tests

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	sailune "github.com/styxnanda/sailune-go"
)

// Exercise the original import path and struct literals from outside the
// implementation packages, as a separate Go client would use them.
type metadataFetcher struct{}

func (metadataFetcher) Fetch(context.Context, string) (sailune.Metadata, error) {
	return sailune.Metadata{Title: "Source title", Authors: []string{"Source author"}, Chapters: 4}, nil
}

var _ sailune.MetadataFetcher = (*sailune.Scraper)(nil)

func TestPublicAPICompatibility(t *testing.T) {
	l := sailune.Library{Store: sailune.Store{Path: filepath.Join(t.TempDir(), "bookmarks.json")}}
	b, err := l.AddScraped(context.Background(), sailune.Bookmark{URL: "https://archiveofourown.org/works/123", Status: sailune.Reading, Chapter: 2}, metadataFetcher{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Site != sailune.AO3 || b.Metadata.Chapters != 4 || b.Title != "Source title" {
		t.Fatalf("unexpected bookmark: %+v", b)
	}
	_, err = l.Add(sailune.Bookmark{URL: b.URL})
	if !errors.Is(err, sailune.ErrDuplicate) {
		t.Fatalf("public error identity lost: %v", err)
	}
	notes := "My notes"
	updated, err := l.Update(b.ID, sailune.Patch{Notes: &notes})
	if err != nil || updated.Metadata == nil || updated.Notes != notes {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, err := l.Get(999); !errors.Is(err, sailune.ErrNotFound) {
		t.Fatalf("not found identity lost: %v", err)
	}
	if err := l.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	list, err := l.List(sailune.Filter{Site: sailune.FFN})
	if err != nil || len(list) != 0 {
		t.Fatalf("list: %+v %v", list, err)
	}
	spec, err := sailune.ParseBrowserSpec("brave:Default")
	if err != nil || spec != (sailune.BrowserSpec{Name: "brave", Profile: "Default"}) {
		t.Fatalf("browser API: %+v %v", spec, err)
	}
	u, err := sailune.LoginURL(sailune.FFN)
	if err != nil || u != "https://www.fanfiction.net/" {
		t.Fatalf("login API: %s %v", u, err)
	}
}
