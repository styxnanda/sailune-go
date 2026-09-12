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

func TestPublicReadingAndCustomizationAPI(t *testing.T) {
	l := sailune.Library{Store: sailune.Store{Path: filepath.Join(t.TempDir(), "library.json")}}
	b, err := l.Add(sailune.Bookmark{URL: "https://fanfiction.net/s/1/1"})
	if err != nil {
		t.Fatal(err)
	}
	rating, read, published, review := 4, 2, 5, "Review"
	b, err = l.Update(b.ID, sailune.Patch{Rating: &rating, Chapter: &read, ReviewNotes: &review, Overrides: &sailune.MetadataPatch{Chapters: &published}})
	if err != nil || b.ReadingProgress().Unread != 3 || b.EffectiveMetadata().Chapters != 5 {
		t.Fatal(b, err)
	}
	u, err := l.ResumeURL(b.ID)
	if err != nil || u != "https://www.fanfiction.net/s/1/3" {
		t.Fatal(u, err)
	}
	items, err := l.List(sailune.Filter{MinRating: 4, Unread: true, Sort: "last-read", Desc: true})
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
}
