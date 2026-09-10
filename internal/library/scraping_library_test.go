package library

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/styxnanda/sailune-go/internal/scrape"
)

type fetchFunc func(context.Context, string) (Metadata, error)

func (f fetchFunc) Fetch(ctx context.Context, u string) (Metadata, error) { return f(ctx, u) }

func TestAddScrapedPreservesPersonalFieldsAndRejectsFailure(t *testing.T) {
	l := testLibrary(t)
	calls := 0
	fetch := fetchFunc(func(ctx context.Context, u string) (Metadata, error) {
		calls++
		return Metadata{Title: "From source", Authors: []string{"First", "Second"}, Summary: "Space adventure", Tags: []string{"Source tag"}, Chapters: 8, Complete: true}, nil
	})
	b, err := l.AddScraped(context.Background(), Bookmark{URL: "https://archiveofourown.org/works/1", Title: "Personal title", Tags: []string{"Favorite"}, Status: Reading, Chapter: 2, Notes: "My note"}, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Personal title" || b.Author != "First, Second" || b.Metadata.Title != "From source" || b.Tags[0] != "Favorite" || b.Chapter != 2 || b.Status != Reading || b.Notes != "My note" {
		t.Fatalf("merged bookmark: %+v", b)
	}
	if _, err := l.AddScraped(context.Background(), Bookmark{URL: "https://archiveofourown.org/works/1/chapters/2"}, fetch); !errors.Is(err, ErrDuplicate) || calls != 1 {
		t.Fatalf("duplicate fetched: %d %v", calls, err)
	}
	if _, err := l.AddScraped(context.Background(), Bookmark{URL: "https://archiveofourown.org/works/2", Chapter: -1}, fetch); err == nil || calls != 1 {
		t.Fatal("invalid bookmark fetched")
	}
	before, _ := os.ReadFile(l.Store.Path)
	fail := fetchFunc(func(context.Context, string) (Metadata, error) { return Metadata{}, scrape.ErrLoginRequired })
	if _, err := l.AddScraped(context.Background(), Bookmark{URL: "https://archiveofourown.org/works/2"}, fail); !errors.Is(err, scrape.ErrLoginRequired) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(l.Store.Path)
	if string(before) != string(after) {
		t.Fatal("failed fetch modified library")
	}
	results, err := l.List(Filter{Query: "Space adventure"})
	if err != nil || len(results) != 1 {
		t.Fatalf("source search: %d %v", len(results), err)
	}
	notes := "Edited"
	b, err = l.Update(b.ID, Patch{Notes: &notes})
	if err != nil || b.Metadata == nil || b.Metadata.Chapters != 8 {
		t.Fatalf("metadata lost on update: %+v %v", b, err)
	}
}

func TestRefreshPreservesPersonalFieldsAndConcurrentEdits(t *testing.T) {
	l := testLibrary(t)
	original, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/123", Title: "My title", Author: "My author", Status: Reading, Chapter: 3, Tags: []string{"favorite"}, Notes: "before"})
	if err != nil {
		t.Fatal(err)
	}
	fetch := fetchFunc(func(ctx context.Context, u string) (Metadata, error) {
		if u != original.URL {
			t.Fatalf("wrong URL %s", u)
		}
		notes := "edited during fetch"
		if _, err := l.Update(original.ID, Patch{Notes: &notes}); err != nil {
			t.Fatal(err)
		}
		return Metadata{Title: "Source title", Authors: []string{"Source author"}, Chapters: 12, Words: 42000, Complete: true}, nil
	})
	b, err := l.Refresh(context.Background(), original.ID, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if b.Metadata.Chapters != 12 || b.Metadata.Words != 42000 || !b.Metadata.Complete || b.Title != original.Title || b.Author != original.Author || b.Status != Reading || b.Chapter != 3 || b.Tags[0] != "favorite" || b.Notes != "edited during fetch" || b.CreatedAt != original.CreatedAt {
		t.Fatalf("refresh changed personal fields or missed metadata: %+v", b)
	}
	saved, err := l.Get(b.ID)
	if err != nil || saved.Metadata.Chapters != 12 {
		t.Fatalf("not persisted: %+v %v", saved, err)
	}
}

func TestRefreshFailuresDoNotModifyLibrary(t *testing.T) {
	l := testLibrary(t)
	b, err := l.Add(Bookmark{URL: "https://archiveofourown.org/works/1"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(l.Store.Path)
	fail := fetchFunc(func(context.Context, string) (Metadata, error) { return Metadata{}, scrape.ErrMetadata })
	if _, err := l.Refresh(context.Background(), b.ID, fail); !errors.Is(err, scrape.ErrMetadata) {
		t.Fatal(err)
	}
	if _, err := l.Refresh(context.Background(), b.ID, nil); err == nil {
		t.Fatal("accepted nil fetcher")
	}
	never := fetchFunc(func(context.Context, string) (Metadata, error) { t.Fatal("unexpected fetch"); return Metadata{}, nil })
	if _, err := l.Refresh(context.Background(), 999, never); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Refresh(ctx, b.ID, never); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	canceled := fetchFunc(func(context.Context, string) (Metadata, error) { cancel(); return Metadata{Chapters: 9}, nil })
	if _, err := l.Refresh(ctx, b.ID, canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(l.Store.Path)
	if string(before) != string(after) {
		t.Fatal("failure modified library")
	}
	deleted := fetchFunc(func(context.Context, string) (Metadata, error) {
		if err := l.Delete(b.ID); err != nil {
			t.Fatal(err)
		}
		return Metadata{Chapters: 9}, nil
	})
	if _, err := l.Refresh(context.Background(), b.ID, deleted); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
