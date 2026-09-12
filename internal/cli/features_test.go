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
)

func TestFeatureCLIWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	run := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if err := Run(append([]string{"--data", path}, args...), &out, &stderr); err != nil {
			t.Fatalf("%v: %v %s", args, err, &stderr)
		}
		return out.String()
	}
	run("add", "https://fanfiction.net/s/1/1", "--no-fetch", "--title", "Story")
	run("update", "1", "--chapter", "3", "--chapters", "10", "--total-chapters", "12", "--rating", "4", "--review-notes", "Wonderful", "--notes", "Private", "--added", "2020-01-01", "--last-read", "2020-02-02", "--summary", "Custom", "--fandoms", "Magic", "--source-tags", "AU", "--language", "English", "--content-rating", "Teen", "--words", "1000", "--complete=false", "--published", "2019-01-01", "--source-updated", "2020-01-02")
	var b bookmarkOutput
	if err := json.Unmarshal([]byte(run("show", "1", "--json")), &b); err != nil {
		t.Fatal(err)
	}
	if b.Rating != 4 || b.ReviewNotes != "Wonderful" || b.Notes != "Private" || b.Progress.Unread != 7 || b.EffectiveMetadata.Summary != "Custom" {
		t.Fatalf("%+v", b)
	}
	if got := run("list", "--query", "story wonderful", "--fandom", "magic", "--language", "english", "--source-tag", "AU", "--complete=false", "--min-rating", "4", "--unread", "--sort", "last-read", "--desc"); !strings.Contains(got, "3/10") || !strings.Contains(got, "2020-01-01") || !strings.Contains(got, "2020-02-02") {
		t.Fatal(got)
	}
	if got := run("resume", "1", "--print-url"); strings.TrimSpace(got) != "https://www.fanfiction.net/s/1/4" {
		t.Fatal(got)
	}
	if got := run("open", "1", "--chapter", "2", "--print-url", "--json"); !strings.Contains(got, `"opened": false`) || !strings.Contains(got, "/s/1/2") {
		t.Fatal(got)
	}
	run("update", "1", "--rating", "0", "--review-notes", "", "--last-read", "", "--reset-overrides", "summary,words")
	for _, args := range [][]string{
		{"update", "1", "--rating", "6"}, {"update", "1", "--last-read", "yesterday"}, {"update", "1", "--added", ""},
		{"update", "1", "--words", "-1"}, {"update", "1", "--published", "bad"}, {"list", "--sort", "bad"},
		{"open", "1", "--next", "--chapter", "2", "--print-url"}, {"open", "1", "--chapter", "0", "--print-url"},
	} {
		var out, stderr bytes.Buffer
		if err := Run(append([]string{"--data", path}, args...), &out, &stderr); err == nil {
			t.Fatal("accepted", args)
		}
	}
}

func TestOpenUsesInjectedBrowserAndPreservesProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	var out, stderr bytes.Buffer
	if err := Run([]string{"--data", path, "add", "https://fanfiction.net/s/1/1", "--no-fetch"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	calls := 0
	deps := loginDependencies{openStory: func(_ context.Context, u string) error {
		calls++
		if u != "https://www.fanfiction.net/s/1/1" {
			t.Fatal(u)
		}
		return nil
	}}
	out.Reset()
	if err := runWithLogin(context.Background(), []string{"--data", path, "resume", "1", "--json"}, &out, &stderr, nil, deps); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(out.String(), `"opened": true`) {
		t.Fatal(calls, out.String())
	}
	deps.openStory = func(context.Context, string) error { return errors.New("browser unavailable") }
	if err := runWithLogin(context.Background(), []string{"--data", path, "open", "1"}, &out, &stderr, nil, deps); err == nil {
		t.Fatal("browser failure hidden")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("open mutated bookmark")
	}
}
