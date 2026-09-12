package library

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Run with -bench SQLite -benchmem. Dataset setup is excluded from timings.
func BenchmarkSQLite(b *testing.B) {
	l := Library{Store: Store{Path: b.TempDir() + "/library.sqlite3"}}
	records := make([]Bookmark, 10000)
	for i := range records {
		records[i] = Bookmark{ID: int64(i + 1), URL: fmt.Sprintf("https://archiveofourown.org/works/%d", i+1), Site: AO3, WorkID: fmt.Sprint(i + 1), Status: Planned, Title: fmt.Sprintf("Story %05d", i), Notes: "some private notes", CreatedAt: time.Unix(int64(i), 0).UTC(), Metadata: &Metadata{Summary: "A story about friendship and magic.", Words: 10000}}
	}
	raw, _ := json.Marshal(Transfer{Version: 1, NextID: 10001, Bookmarks: records})
	if _, err := l.Import(bytes.NewReader(raw), false); err != nil {
		b.Fatal(err)
	}
	b.Run("Page20_10k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := l.List(Filter{Limit: 20}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Get_10k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := l.Get(5000); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Search_10k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := l.List(Filter{Query: "magic", Limit: 20}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("NoMatchSearch_10k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := l.List(Filter{Query: "absent-term", Limit: 20}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Update_10k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := l.Update(5000, Patch{Rating: ptr(i % 6)}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
