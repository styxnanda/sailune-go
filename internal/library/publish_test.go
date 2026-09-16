package library

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPublishDatabaseNeverReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("new database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("existing database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishDatabase(source, destination); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected existing-file error, got %v", err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "existing database" {
		t.Fatalf("destination changed: %q, %v", data, err)
	}
}

func TestConcurrentFirstWritesPreserveBothBookmarks(t *testing.T) {
	library := Library{Store: Store{Path: filepath.Join(t.TempDir(), "library.sqlite3")}}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, url := range []string{"https://archiveofourown.org/works/1", "https://archiveofourown.org/works/2"} {
		wg.Add(1)
		go func(url string) { defer wg.Done(); _, err := library.Add(Bookmark{URL: url}); errors <- err }(url)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := library.List(Filter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("want both committed bookmarks, got %d, %v", len(rows), err)
	}
}
