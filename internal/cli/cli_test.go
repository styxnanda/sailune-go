package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sailune "github.com/styxnanda/sailune-go"
)

func TestBrowserCookieCLI(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(profile, "cookies.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		"CREATE TABLE moz_cookies(host TEXT,name TEXT,value TEXT,path TEXT,expiry INTEGER,isSecure INTEGER,isHttpOnly INTEGER,originAttributes TEXT)",
		"INSERT INTO moz_cookies VALUES ('.archiveofourown.org','login','AO3_SECRET','/',0,1,1,'')",
		"INSERT INTO moz_cookies VALUES ('.fanfiction.net','login','FFN_SECRET','/',0,1,1,'')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "library.json")
	fetcher := &testFetcher{}
	call := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if err := run(context.Background(), append([]string{"--data", path}, args...), &out, &stderr, fetcher); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String()+stderr.String(), "SECRET") {
			t.Fatal("browser cookie leaked to output")
		}
		return out.String()
	}
	spec := "firefox:" + profile
	call("auth", "ao3", "--cookies-from-browser", spec, "--json")
	call("add", "https://www.fanfiction.net/s/123/1", "--cookies-from-browser", spec, "--json")
	if fetcher.calls != 1 {
		t.Fatal("fetch not called after browser import")
	}
	if got := call("auth", "ffn", "--json"); !strings.Contains(got, `"configured": true`) {
		t.Fatal("add did not save imported session")
	}
	for _, args := range [][]string{
		{"auth", "ao3", "--cookies-from-browser", spec, "--clear"},
		{"auth", "ao3", "--cookies-from-browser", spec, "--cookies", "missing"},
		{"add", "https://archiveofourown.org/works/123", "--cookies-from-browser", spec, "--no-fetch"},
	} {
		var out, stderr bytes.Buffer
		if err := run(context.Background(), append([]string{"--data", path}, args...), &out, &stderr, fetcher); err == nil {
			t.Fatalf("accepted conflicting flags: %v", args)
		}
	}
	var out, stderr bytes.Buffer
	err = run(context.Background(), []string{"--data", path, "add", "https://www.fanfiction.net/s/123/1", "--cookies-from-browser", "brave:/missing/profile"}, &out, &stderr, fetcher)
	if !errors.Is(err, sailune.ErrDuplicate) {
		t.Fatalf("duplicate accessed browser profile: %v", err)
	}
}

type testFetcher struct {
	calls int
	fail  bool
}

func (f *testFetcher) Fetch(context.Context, string) (sailune.Metadata, error) {
	f.calls++
	if f.fail {
		return sailune.Metadata{}, sailune.ErrLoginRequired
	}
	return sailune.Metadata{Title: "Fetched title", Authors: []string{"Fetched author"}, Summary: "A summary", Chapters: 5}, nil
}

func TestDefaultFetchAndOfflineEscape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	fetcher := &testFetcher{}
	var out, stderr bytes.Buffer
	call := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		return run(context.Background(), append([]string{"--data", path}, args...), &out, &stderr, fetcher)
	}
	if err := call("add", "https://archiveofourown.org/works/1", "--json"); err != nil {
		t.Fatal(err)
	}
	var b sailune.Bookmark
	if err := json.Unmarshal(out.Bytes(), &b); err != nil || b.Title != "Fetched title" || b.Author != "Fetched author" || b.Metadata == nil || fetcher.calls != 1 {
		t.Fatalf("default fetch: %+v %v", b, err)
	}
	fetcher.fail = true
	if err := call("add", "https://fanfiction.net/s/2/1", "--json"); !errors.Is(err, sailune.ErrLoginRequired) || out.Len() != 0 {
		t.Fatalf("failure: %v %s", err, &out)
	}
	if err := call("add", "https://fanfiction.net/s/2/1", "--no-fetch", "--title", "Offline"); err != nil || fetcher.calls != 2 {
		t.Fatalf("offline: %v calls=%d", err, fetcher.calls)
	}
	if err := call("show", "1"); err != nil || !strings.Contains(out.String(), "A summary") {
		t.Fatalf("display: %v %s", err, &out)
	}
}

func TestAuthCommandsNeverPrintSecrets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cookies.txt")
	data := ".archiveofourown.org\tTRUE\t/\tTRUE\t0\tsession\tTEST_SECRET\n.fanfiction.net\tTRUE\t/\tTRUE\t0\tsession\tFFN_SECRET\n"
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, site := range []string{"ao3", "ffn"} {
		for _, args := range [][]string{{"auth", site, "--cookies", file, "--json"}, {"auth", site}, {"auth", site, "--clear", "--json"}} {
			var out, stderr bytes.Buffer
			if err := Run(append([]string{"--sessions", filepath.Join(dir, "sessions")}, args...), &out, &stderr); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String()+stderr.String(), "SECRET") {
				t.Fatal("session secret printed")
			}
		}
	}
	var out, stderr bytes.Buffer
	if err := Run([]string{"auth", "ao3", "--cookies", file, "--clear"}, &out, &stderr); err == nil {
		t.Fatal("accepted conflicting auth flags")
	}
}

func TestCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	run := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if err := Run(append([]string{"--data", path}, args...), &out, &stderr); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, &stderr)
		}
		return out.String()
	}
	if got := run("list", "--json"); strings.TrimSpace(got) != "[]" {
		t.Fatal(got)
	}
	var b sailune.Bookmark
	if err := json.Unmarshal([]byte(run("add", "https://m.fanfiction.net/s/42/3/Title", "--no-fetch", "--title", "Some Story", "--author=Writer", "--json")), &b); err != nil {
		t.Fatal(err)
	}
	if b.ID != 1 || b.Title != "Some Story" || b.URL != "https://www.fanfiction.net/s/42/1" {
		t.Fatalf("%+v", b)
	}
	run("update", "--chapter", "3", "1", "--notes", "hello", "--status", "reading")
	if got := run("list", "--query", "SOME", "--site", "ffn"); !strings.Contains(got, "Some Story") {
		t.Fatal(got)
	}
	run("update", "1", "--notes", "")
	if got := run("show", "1", "--json"); !strings.Contains(got, `"notes": ""`) {
		t.Fatal(got)
	}
	if got := run("delete", "1", "--json"); !strings.Contains(got, `"deleted_id": 1`) {
		t.Fatal(got)
	}
	if got := run("list"); !strings.Contains(got, "No bookmarks") {
		t.Fatal(got)
	}
}

func TestErrorsAndHelp(t *testing.T) {
	t.Setenv("SAILUNE_DATA", filepath.Join(t.TempDir(), "library.json"))
	for _, args := range [][]string{
		{"unknown"}, {"add"}, {"show", "0"}, {"show", "x"}, {"show", "999"},
		{"list", "extra"}, {"list", "--site", "other"}, {"list", "--status", "other"},
		{"update", "1"}, {"list", "--unknown"}, {"add", "--title"},
		{"add", "https://archiveofourown.org/works/1", "--chapter", "-1"},
		{"add", "https://archiveofourown.org/works/1", "--status", "invalid"},
	} {
		var out, stderr bytes.Buffer
		if err := Run(args, &out, &stderr); err == nil {
			t.Errorf("expected error for %v", args)
		}
		if out.Len() != 0 {
			t.Errorf("unexpected stdout for %v: %s", args, &out)
		}
	}
	for _, args := range [][]string{nil, {"--help"}, {"help"}, {"add", "--help"}, {"show", "--help"}} {
		var out, stderr bytes.Buffer
		if err := Run(args, &out, &stderr); err != nil || !strings.Contains(out.String()+stderr.String(), "Usage:") {
			t.Fatalf("help %v: %v", args, err)
		}
	}
}

func TestDataPrecedenceAndSafeOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SAILUNE_DATA", filepath.Join(dir, "env.json"))
	var out, stderr bytes.Buffer
	if err := Run([]string{"add", "https://archiveofourown.org/works/1", "--no-fetch", "--title", "Hello\x1b[31m\nworld"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("terminal escape emitted")
	}
	out.Reset()
	if err := Run([]string{"--data", filepath.Join(dir, "other.json"), "list", "--json"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatal("explicit path did not override environment")
	}
}

func TestRefreshCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	fetcher := &testFetcher{}
	var out, stderr bytes.Buffer
	call := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		return run(context.Background(), append([]string{"--data", path}, args...), &out, &stderr, fetcher)
	}
	if err := call("add", "https://fanfiction.net/s/123", "--no-fetch", "--title", "Personal", "--chapter", "2"); err != nil {
		t.Fatal(err)
	}
	if err := call("refresh", "1", "--json"); err != nil {
		t.Fatal(err)
	}
	var b sailune.Bookmark
	if err := json.Unmarshal(out.Bytes(), &b); err != nil || b.Metadata == nil || b.Metadata.Chapters != 5 || b.Chapter != 2 || b.Title != "Personal" {
		t.Fatalf("refresh: %s %v", &out, err)
	}
	fetcher.fail = true
	before, _ := os.ReadFile(path)
	if err := call("refresh", "1"); !errors.Is(err, sailune.ErrLoginRequired) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed refresh modified library")
	}
	calls := fetcher.calls
	for _, args := range [][]string{{"refresh", "999"}, {"refresh", "0"}, {"refresh", "1", "--no-fetch"}, {"refresh", "1", "--chapter", "3"}} {
		if err := call(args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if calls != fetcher.calls {
		t.Fatal("invalid refresh fetched")
	}
}
