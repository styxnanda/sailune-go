package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransferCLI(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.sqlite3")
	dest := filepath.Join(dir, "dest.sqlite3")
	export := filepath.Join(dir, "transfer.json")
	run := func(path string, args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if err := Run(append([]string{"--data", path}, args...), &out, &stderr); err != nil {
			t.Fatal(args, err, &stderr)
		}
		return out.String()
	}
	run(source, "add", "https://fanfiction.net/s/1/1", "--no-fetch", "--title", "Transferred")
	if got := run(source, "export", export, "--json"); !strings.Contains(got, "exported_to") {
		t.Fatal(got)
	}
	if got := run(dest, "import", export, "--json"); !strings.Contains(got, `"imported": 1`) {
		t.Fatal(got)
	}
	if got := run(dest, "import", export, "--merge", "--json"); !strings.Contains(got, `"skipped": 1`) {
		t.Fatal(got)
	}
	raw := run(dest, "export", "-")
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil || snapshot["format"] != "sailune-library" {
		t.Fatal(raw, err)
	}
	if got := run(dest, "show", "1"); !strings.Contains(got, "Transferred") {
		t.Fatal(got)
	}
	old := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(old, []byte(`{"version":1,"next_id":1,"bookmarks":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	run(filepath.Join(dir, "migrated.sqlite3"), "migrate", old)
	if b, err := os.ReadFile(old); err != nil || len(b) == 0 {
		t.Fatal(err)
	}
	for _, command := range []string{"migrate", "import", "export"} {
		run(dest, command, "--help")
	}
}
