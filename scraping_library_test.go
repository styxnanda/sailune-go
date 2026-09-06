package sailune

import (
	"context"
	"errors"
	"os"
	"testing"
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
	fail := fetchFunc(func(context.Context, string) (Metadata, error) { return Metadata{}, ErrLoginRequired })
	if _, err := l.AddScraped(context.Background(), Bookmark{URL: "https://archiveofourown.org/works/2"}, fail); !errors.Is(err, ErrLoginRequired) {
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
