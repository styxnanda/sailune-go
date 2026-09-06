package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sailune "github.com/styxnanda/sailune-go"
)

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
