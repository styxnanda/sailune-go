package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sailune "github.com/styxnanda/sailune-go"
)

func TestLoginConsentBothSites(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile")
	if err := os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(profile, "cookies.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		"CREATE TABLE moz_cookies(host TEXT,name TEXT,value TEXT,path TEXT,expiry INTEGER,isSecure INTEGER,isHttpOnly INTEGER,originAttributes TEXT)",
		"INSERT INTO moz_cookies VALUES ('.archiveofourown.org','login','SYNTHETIC_SECRET','/',0,1,1,'')",
		"INSERT INTO moz_cookies VALUES ('.fanfiction.net','login','SYNTHETIC_SECRET','/',0,1,1,'')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, site := range []sailune.Site{sailune.AO3, sailune.FFN} {
		for _, mode := range []string{"legacy", "hint", "family"} {
			var out, prompt bytes.Buffer
			store := sailune.SessionStore{Dir: t.TempDir()}
			spec := "firefox:" + profile
			input := "yes\n"
			args := []string{"--sessions", store.Dir, "auth", string(site), "--login", "--json"}
			if mode == "hint" {
				args = append(args, "--cookies-from-browser", spec)
			} else {
				input = spec + "\nyes\n"
				if mode == "family" {
					input = "gecko\n" + spec + "\nyes\n"
				}
			}
			opened := 0
			deps := loginDependencies{input: strings.NewReader(input), open: func(ctx context.Context, got sailune.Site) error {
				opened++
				if got != site {
					t.Fatal("opened wrong site")
				}
				status, err := store.Status(site)
				if err != nil || status.Configured {
					t.Fatal("import occurred before consent")
				}
				return nil
			}}
			err := runWithLogin(context.Background(), args, &out, &prompt, nil, deps)
			if err != nil || opened != 1 || !strings.Contains(out.String(), `"configured": true`) {
				t.Fatalf("login: %v %s", err, &out)
			}
			if !strings.Contains(prompt.String(), "Type yes") || !strings.Contains(prompt.String(), "without encryption") {
				t.Fatal("missing consent information")
			}
			if strings.Contains(out.String()+prompt.String(), "SYNTHETIC_SECRET") {
				t.Fatal("cookie value printed")
			}
		}
	}
}

func TestLoginCancelDoesNotImport(t *testing.T) {
	for _, input := range []string{"", "\n", "no\n", "cancel\n"} {
		store := sailune.SessionStore{Dir: t.TempDir()}
		if _, err := store.Import(sailune.AO3, strings.NewReader(".archiveofourown.org\tTRUE\t/\tTRUE\t0\tlogin\tEXISTING\n")); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(store.Dir, "ao3.json")
		before, _ := os.ReadFile(path)
		var prompt bytes.Buffer
		_, err := interactiveLogin(context.Background(), sailune.AO3, "brave:/nonexistent", store, &prompt, loginDependencies{input: strings.NewReader(input), open: func(context.Context, sailune.Site) error { return nil }})
		if !errors.Is(err, errLoginCanceled) {
			t.Fatalf("input %q: %v", input, err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("canceled login changed saved session")
		}
	}
}

func TestLoginOpenFailureAndInvalidOptions(t *testing.T) {
	openErr := errors.New("could not open browser")
	var prompt bytes.Buffer
	_, err := interactiveLogin(context.Background(), sailune.AO3, "brave", sailune.SessionStore{Dir: t.TempDir()}, &prompt, loginDependencies{input: strings.NewReader("yes\n"), open: func(context.Context, sailune.Site) error { return openErr }})
	if !errors.Is(err, openErr) {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"auth", "ao3", "--login", "--cookies", "missing"},
		{"auth", "ao3", "--login", "--clear"},
		{"auth", "invalid", "--login"},
	} {
		var out, stderr bytes.Buffer
		err := runWithLogin(context.Background(), args, &out, &stderr, nil, loginDependencies{input: strings.NewReader("yes\n"), open: func(context.Context, sailune.Site) error { t.Fatal("browser opened for invalid flags"); return nil }})
		if err == nil {
			t.Fatal("invalid login options accepted")
		}
	}
}

func TestLoginContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		_, err := interactiveLogin(ctx, sailune.AO3, "brave:/nonexistent", sailune.SessionStore{Dir: t.TempDir()}, io.Discard, loginDependencies{input: reader, open: func(context.Context, sailune.Site) error { cancel(); return nil }})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("login ignored cancellation")
	}
}

func TestLoginFamilySelection(t *testing.T) {
	for _, tc := range []struct{ input, source string }{
		{"chromium\nbrave:Default\nno\n", "chromium/brave:Default"},
		{"chromium\nopera\nno\n", "chromium/opera"},
		{"gecko\nfirefox:/fork/profile\nno\n", "gecko/firefox:/fork/profile"},
		{"chromium\n\n", ""},
		{"gecko\ncancel\n", ""},
	} {
		var prompt bytes.Buffer
		store := sailune.SessionStore{Dir: t.TempDir()}
		_, err := interactiveLogin(context.Background(), sailune.FFN, "", store, &prompt, loginDependencies{
			input: strings.NewReader(tc.input), open: func(context.Context, sailune.Site) error { return nil },
		})
		if !errors.Is(err, errLoginCanceled) {
			t.Fatalf("%q: %v", tc.input, err)
		}
		if tc.source != "" && !strings.Contains(prompt.String(), "from "+tc.source) {
			t.Fatalf("incorrect source: %s", &prompt)
		}
		status, err := store.Status(sailune.FFN)
		if err != nil || status.Configured {
			t.Fatal("selection without consent imported cookies")
		}
	}
}
