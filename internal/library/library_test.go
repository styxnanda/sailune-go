package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func testLibrary(t *testing.T) Library {
	t.Helper()
	return Library{Store: Store{Path: filepath.Join(t.TempDir(), "nested", "bookmarks.json")}}
}

func TestLibraryLifecycle(t *testing.T) {
	l := testLibrary(t)
	empty, err := l.List(Filter{})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list: %v %v", empty, err)
	}
	b, err := l.Add(Bookmark{URL: "https://archiveofourown.org/works/123/chapters/456", Title: " A Story ", Author: "Writer", Tags: []string{" Magic ", "magic", "", "AU"}})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != 1 || b.Status != Planned || b.Chapter != 0 || b.Title != "A Story" || !reflect.DeepEqual(b.Tags, []string{"Magic", "AU"}) || b.CreatedAt.IsZero() {
		t.Fatalf("unexpected: %+v", b)
	}
	if _, err := l.Add(Bookmark{URL: "http://archiveofourown.org/works/123?view_adult=true"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	status, chapter, notes := Reading, 4, "Next weekend"
	updated, err := l.Update(b.ID, Patch{Status: &status, Chapter: &chapter, Notes: &notes})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != b.Title || !updated.CreatedAt.Equal(b.CreatedAt) || updated.UpdatedAt.Before(b.UpdatedAt) || updated.Chapter != 4 {
		t.Fatalf("update: %+v", updated)
	}
	l = Library{Store: Store{Path: l.Store.Path}}
	matches, err := l.List(Filter{Query: "WEEKEND", Tag: "magic", Site: AO3, Status: Reading})
	if err != nil || len(matches) != 1 {
		t.Fatalf("filter: %v %v", matches, err)
	}
	none, err := l.List(Filter{Site: FFN})
	if err != nil || len(none) != 0 {
		t.Fatalf("site: %v %v", none, err)
	}
	clear := ""
	if _, err := l.Update(b.ID, Patch{Notes: &clear}); err != nil {
		t.Fatal(err)
	}
	got, err := l.Get(b.ID)
	if err != nil || got.Notes != "" || got.Author != "Writer" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if err := l.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := l.Delete(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	next, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/123/1/Title"})
	if err != nil || next.ID != 2 {
		t.Fatalf("ID reuse: %+v %v", next, err)
	}
}

func TestFailedMutationPreservesFile(t *testing.T) {
	l := testLibrary(t)
	if _, err := l.Add(Bookmark{URL: "https://archiveofourown.org/works/1"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(l.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	negative, invalid := -1, Status("unknown")
	for _, patch := range []Patch{{Chapter: &negative}, {Status: &invalid}} {
		if _, err := l.Update(1, patch); err == nil {
			t.Fatal("accepted invalid update")
		}
	}
	if _, err := l.Update(99, Patch{}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(l.Store.Path)
	if string(before) != string(after) {
		t.Fatal("failed mutation modified library")
	}

}

func TestCorruptStoreIsNotOverwritten(t *testing.T) {
	for _, content := range []string{"", "{", "null", `{}`, `{"version":99,"next_id":1,"bookmarks":[]}`, `{"version":1,"next_id":1,"bookmarks":[{"id":2}]}`} {
		t.Run(content, func(t *testing.T) {
			l := testLibrary(t)
			if err := os.MkdirAll(filepath.Dir(l.Store.Path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(l.Store.Path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Add(Bookmark{URL: "https://archiveofourown.org/works/1"}); err == nil {
				t.Fatal("accepted corrupt store")
			}
			got, err := os.ReadFile(l.Store.Path)
			if err != nil || string(got) != content {
				t.Fatalf("corrupt data overwritten: %q %v", got, err)
			}
		})
	}
}

func TestConcurrentWritersDoNotLoseSuccessfulAdds(t *testing.T) {
	l := testLibrary(t)
	const writers = 16
	results := make(chan error, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 1; i <= writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			_, err := l.Add(Bookmark{URL: fmt.Sprintf("https://archiveofourown.org/works/%d", id)})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	bookmarks, err := l.List(Filter{})
	if err != nil || successes == 0 || len(bookmarks) != successes {
		t.Fatalf("%d successful writes, %d stored bookmarks, error: %v", successes, len(bookmarks), err)
	}
	// The last successful writer must release its lock.
	if _, err := l.Add(Bookmark{URL: "https://archiveofourown.org/works/999"}); err != nil {
		t.Fatal(err)
	}
}
