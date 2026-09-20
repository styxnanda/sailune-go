package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOrganizationCommands(t *testing.T) {
	data := filepath.Join(t.TempDir(), "library.db")
	run := func(args ...string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		if e := Run(append([]string{"--data", data}, args...), &out, &errOut); e != nil {
			t.Fatal(e, errOut.String())
		}
		return out.String()
	}
	run("add", "https://archiveofourown.org/works/123", "--no-fetch")
	raw := run("collection", "create", "--name", "trauma-inducing")
	var c struct {
		ID string `json:"id"`
	}
	if e := json.Unmarshal([]byte(raw), &c); e != nil {
		t.Fatal(e)
	}
	run("collection", "add", c.ID, "1")
	run("list", "--collection", c.ID, "--json")
	backup := filepath.Join(t.TempDir(), "library.zip")
	run("export", backup)
	run("import", backup, "--merge")
	run("collection", "remove", c.ID, "1")
	run("collection", "delete", c.ID)
	if run("--version") != "0.9.0\n" {
		t.Fatal("wrong release version")
	}
}
