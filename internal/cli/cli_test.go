package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	sailune "github.com/styxnanda/sailune-go"
)

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
	if err := json.Unmarshal([]byte(run("add", "https://m.fanfiction.net/s/42/3/Title", "--title", "Some Story", "--author=Writer", "--json")), &b); err != nil {
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
	if err := Run([]string{"add", "https://archiveofourown.org/works/1", "--title", "Hello\x1b[31m\nworld"}, &out, &stderr); err != nil {
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
