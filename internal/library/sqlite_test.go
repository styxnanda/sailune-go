package library

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSQLiteRoundTripPreservesAllDataAndWatermark(t *testing.T) {
	source := testLibrary(t)
	b, err := source.Add(Bookmark{URL: "https://archiveofourown.org/works/42", Title: "Custom", Notes: "Private", ReviewNotes: "Review", Rating: 5, Chapter: 2, Metadata: &Metadata{Chapters: 4, ChapterIDs: []string{"91", "80", "70", "60"}}, Overrides: &MetadataPatch{Words: ptr(1234)}})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := source.Add(Bookmark{URL: "https://fanfiction.net/s/2/1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Delete(deleted.ID); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := source.Export(&encoded); err != nil {
		t.Fatal(err)
	}
	dest := testLibrary(t)
	result, err := dest.Import(&encoded, false)
	if err != nil || result.Imported != 1 {
		t.Fatal(result, err)
	}
	got, err := dest.Get(b.ID)
	if err != nil || !reflect.DeepEqual(got, b) {
		t.Fatal(got, b, err)
	}
	next, err := dest.Add(Bookmark{URL: "https://fanfiction.net/s/3/1"})
	if err != nil || next.ID != 3 {
		t.Fatal(next, err)
	}
	file, err := os.ReadFile(dest.Store.Path)
	if err != nil || string(file[:16]) != "SQLite format 3\x00" {
		t.Fatal("not sqlite", err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(dest.Store.Path)
		if info.Mode().Perm() != 0600 {
			t.Fatal(info.Mode())
		}
	}
}

func TestImportMergeAndInvalidInputDoNotOverwrite(t *testing.T) {
	l := testLibrary(t)
	original, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1", Notes: "keep", Rating: 4})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Transfer{Format: "sailune-library", Version: 1, NextID: 9, Bookmarks: []Bookmark{
		{ID: 7, URL: original.URL, Site: FFN, WorkID: "1", Status: Planned, Notes: "overwrite"},
		{ID: 8, URL: "https://archiveofourown.org/works/2", Site: AO3, WorkID: "2", Status: Reading, Chapter: 1},
	}}
	raw, _ := json.Marshal(snapshot)
	if _, err := l.Import(bytes.NewReader(raw), false); err == nil {
		t.Fatal("restore overwrote populated library")
	}
	result, err := l.Import(bytes.NewReader(raw), true)
	if err != nil || result.Imported != 1 || result.Skipped != 1 {
		t.Fatal(result, err)
	}
	b, err := l.Get(1)
	if err != nil || b.Notes != "keep" || b.Rating != 4 {
		t.Fatal(b, err)
	}
	b, err = l.Get(2)
	if err != nil || b.WorkID != "2" || b.Chapter != 1 {
		t.Fatal(b, err)
	}
	before, _ := os.ReadFile(l.Store.Path)
	for _, invalid := range []string{`{}`, `[]`, `null`, string(raw) + `{}`, strings.Replace(string(raw), `"version":1`, `"version":99`, 1), strings.Replace(string(raw), `"chapter":1`, `"chapter":-1`, 1), strings.Replace(string(raw), `"format":"sailune-library"`, `"format":"future"`, 1), strings.Replace(string(raw), `"next_id":9`, `"next_id":8`, 1), strings.Replace(string(raw), `"version":1`, `"version":1,"secret":"unexpected"`, 1)} {
		if _, err := l.Import(strings.NewReader(invalid), true); err == nil {
			t.Fatal("accepted", invalid)
		}
	}
	after, _ := os.ReadFile(l.Store.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid import wrote data")
	}
}

func TestLegacyMigrationKeepsSourceAndRejectsInPlace(t *testing.T) {
	l := testLibrary(t)
	old := `{"version":1,"next_id":6,"bookmarks":[{"id":3,"url":"https://archiveofourown.org/works/1","site":"ao3","work_id":"1","status":"planned","chapter":0,"tags":[]}]}`
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := l.ImportFile(path, false)
	if err != nil || result.Imported != 1 {
		t.Fatal(result, err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != old {
		t.Fatal("legacy source modified")
	}
	next, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/6/1"})
	if err != nil || next.ID != 6 {
		t.Fatal(next, err)
	}
	bad := Library{Store: Store{Path: path}}
	if _, err := bad.ImportFile(path, false); err == nil {
		t.Fatal("in-place migration allowed")
	}
}

func TestExportPublicationAndAliasing(t *testing.T) {
	l := testLibrary(t)
	if _, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "transfer.json")
	if err := l.ExportFile(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := l.ExportFile(path); err == nil {
		t.Fatal("overwrote export")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("export changed")
	}
	if err := l.ExportFile(l.Store.Path); err == nil {
		t.Fatal("overwrote DB")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Link(l.Store.Path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ImportFile(alias, true); err == nil {
		t.Fatal("imported own DB alias")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal(info.Mode())
		}
	}
}

func TestSQLiteRollbackAndSnapshotIsolation(t *testing.T) {
	l := testLibrary(t)
	if _, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1", Notes: "before"}); err != nil {
		t.Fatal(err)
	}
	db, err := l.Store.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	b, err := getBookmark(tx, 1)
	if err != nil {
		t.Fatal(err)
	}
	b.Notes = "uncommitted"
	if err := saveBookmark(tx, b); err != nil {
		t.Fatal(err)
	}
	got, err := l.Get(1)
	if err != nil || got.Notes != "before" {
		t.Fatal("dirty read", got, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	got, err = l.Get(1)
	if err != nil || got.Notes != "before" {
		t.Fatal(got, err)
	}
	// Force an error mid-import after the first insert; the transaction must undo it.
	if _, err = db.Exec(`CREATE TRIGGER fail_second BEFORE INSERT ON bookmarks WHEN NEW.url='https://fanfiction.net/s/3/1' BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	payload := Transfer{Version: 1, NextID: 4, Bookmarks: []Bookmark{{ID: 2, URL: "https://fanfiction.net/s/2/1", Site: FFN, WorkID: "2", Status: Planned}, {ID: 3, URL: "https://fanfiction.net/s/3/1", Site: FFN, WorkID: "3", Status: Planned}}}
	raw, _ := json.Marshal(payload)
	if _, err := l.Import(bytes.NewReader(raw), true); err == nil {
		t.Fatal("trigger did not abort")
	}
	records, err := l.List(Filter{})
	if err != nil || len(records) != 1 {
		t.Fatal("partial import", records, err)
	}
}

func TestSQLiteSQLParametersAndIndexes(t *testing.T) {
	l := testLibrary(t)
	if _, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1", Title: "Robert'); DROP TABLE bookmarks;--", Tags: []string{"Σ"}}); err != nil {
		t.Fatal(err)
	}
	got, err := l.List(Filter{Query: "DROP TABLE", Tag: "ς"})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	got, err = l.List(Filter{Query: "' OR 1=1 --"})
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	db, err := l.Store.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("EXPLAIN QUERY PLAN SELECT payload FROM bookmarks ORDER BY added,id LIMIT 20")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "bookmarks_added") {
			found = true
		}
	}
	if !found {
		t.Fatal("sorted pagination did not use index")
	}
}

func TestSQLiteWriterContentionUsesBusyTimeout(t *testing.T) {
	l := testLibrary(t)
	if _, err := l.Add(Bookmark{URL: "https://fanfiction.net/s/1/1"}); err != nil {
		t.Fatal(err)
	}
	db, err := l.Store.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := l.Update(1, Patch{Notes: ptr("after lock")}); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("writer bypassed transaction: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	tx.Rollback()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("writer failed to resume")
	}
}

// This also exercises drive-letter URI handling on Windows runners.
func TestSQLiteLibraryPathEscaping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library #100% 日本語.sqlite3")
	lib := Library{Store: Store{Path: path}}
	b, err := lib.Add(Bookmark{URL: "https://archiveofourown.org/works/934", Title: "Exact file path"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	reopened := Library{Store: Store{Path: path}}
	got, err := reopened.Get(b.ID)
	if err != nil || got.Title != b.Title {
		t.Fatalf("reopen: %+v, %v", got, err)
	}
}
