package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestEncryptedSessionAndTamperDetection(t *testing.T) {
	s := SessionStore{Dir: t.TempDir()}
	if _, err := s.Import(AO3, strings.NewReader(cookieExport(".archiveofourown.org", "session", "PRIVATE_COOKIE_VALUE"))); err != nil {
		t.Fatal(err)
	}
	path, _ := s.path(AO3)
	original, _ := os.ReadFile(path)
	if bytes.Contains(original, []byte("PRIVATE_COOKIE_VALUE")) || bytes.Contains(original, []byte("archiveofourown.org")) || bytes.Contains(original, []byte(`"cookies"`)) {
		t.Fatal("plaintext metadata or credential written")
	}
	var saved encryptedSession
	if err := json.Unmarshal(original, &saved); err != nil || saved.Version != 2 {
		t.Fatal("not encrypted")
	}
	if err := s.WithJar(AO3, func(http.CookieJar) error { return nil }); err != nil {
		t.Fatal(err)
	}
	rotated, _ := os.ReadFile(path)
	if bytes.Equal(original, rotated) {
		t.Fatal("nonce reused")
	}
	for _, change := range []func(*encryptedSession){
		func(e *encryptedSession) { e.Ciphertext[0] ^= 1 },
		func(e *encryptedSession) { e.Nonce[0] ^= 1 },
		func(e *encryptedSession) { e.Nonce = nil },
		func(e *encryptedSession) { e.Version = 3 },
		func(e *encryptedSession) { e.KeyID = strings.Repeat("0", 64) },
	} {
		var e encryptedSession
		json.Unmarshal(original, &e)
		change(&e)
		data, _ := json.Marshal(e)
		os.WriteFile(path, data, 0600)
		called := false
		err := s.WithJar(AO3, func(http.CookieJar) error { called = true; return nil })
		if err == nil || called || strings.Contains(err.Error(), "PRIVATE_COOKIE_VALUE") {
			t.Fatal("accepted or leaked tampered data")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(data, after) {
			t.Fatal("tampered file changed")
		}
	}
	ffn, _ := s.path(FFN)
	os.WriteFile(ffn, original, 0600)
	if _, err := s.Status(FFN); err == nil {
		t.Fatal("accepted ciphertext for wrong site")
	}
	os.WriteFile(path, original, 0600)
	keyring.Delete(sessionKeyService, saved.KeyID)
	if _, err := s.Status(AO3); err == nil {
		t.Fatal("accepted missing key")
	}
}

func TestKeyStoreFailureNeverWritesPlaintext(t *testing.T) {
	keyring.MockInitWithError(errors.New("SENSITIVE_BACKEND_ERROR"))
	defer keyring.MockInit()
	s := SessionStore{Dir: t.TempDir()}
	_, err := s.Import(AO3, strings.NewReader(cookieExport(".archiveofourown.org", "session", "SECRET_COOKIE")))
	if err == nil || strings.Contains(err.Error(), "SENSITIVE_BACKEND_ERROR") {
		t.Fatalf("unsafe error: %v", err)
	}
	files, _ := os.ReadDir(s.Dir)
	if len(files) != 0 {
		t.Fatal("failed encryption left files")
	}
}

func writeLegacyFixture(t *testing.T, dir string) string {
	t.Helper()
	// Reuse the parser through a synthetic session structure, never a real export.
	data := []byte(`{"version":1,"cookies":[{"origin":"https://archiveofourown.org/","cookie":{"Name":"session","Value":"LEGACY_SECRET","Path":"/","Secure":true}}]}`)
	path := filepath.Join(dir, "ao3.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSessionMigration(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		source := t.TempDir()
		src := writeLegacyFixture(t, source)
		dest := t.TempDir()
		if inPlace {
			dest = source
		}
		s := SessionStore{Dir: dest}
		if _, err := (SessionStore{Dir: source}).Status(AO3); err == nil {
			t.Fatal("used plaintext without migration")
		}
		status, err := s.Migrate(AO3, source)
		if err != nil || !status.Configured || status.UsableCookies != 1 {
			t.Fatalf("migration: %+v %v", status, err)
		}
		path, _ := s.path(AO3)
		data, _ := os.ReadFile(path)
		if bytes.Contains(data, []byte("LEGACY_SECRET")) {
			t.Fatal("destination is plaintext")
		}
		if !inPlace {
			if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("source not removed")
			}
		}
	}
}

func TestMigrationFailuresPreserveSource(t *testing.T) {
	source := t.TempDir()
	src := writeLegacyFixture(t, source)
	before, _ := os.ReadFile(src)
	s := SessionStore{Dir: t.TempDir()}
	if _, err := s.Import(AO3, strings.NewReader(cookieExport(".archiveofourown.org", "session", "EXISTING"))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(AO3, source); err == nil {
		t.Fatal("overwrote destination")
	}
	keyring.MockInitWithError(errors.New("locked"))
	defer keyring.MockInit()
	if _, err := (SessionStore{Dir: t.TempDir()}).Migrate(AO3, source); err == nil {
		t.Fatal("migrated without key store")
	}
	if _, err := (SessionStore{Dir: source}).Migrate(AO3, source); err == nil {
		t.Fatal("in-place migration without key store")
	}
	after, _ := os.ReadFile(src)
	if !bytes.Equal(before, after) {
		t.Fatal("failed migration removed or changed source")
	}
}

func TestDefaultSessionLocationIndependentOfBookmarks(t *testing.T) {
	t.Setenv("SAILUNE_DATA", filepath.Join(t.TempDir(), "cloud", "bookmarks.json"))
	first, err := DefaultSessionDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SAILUNE_DATA", filepath.Join(t.TempDir(), "other", "bookmarks.json"))
	second, err := DefaultSessionDir()
	if err != nil || first != second {
		t.Fatalf("sessions followed bookmark path: %q %q %v", first, second, err)
	}
	if strings.Contains(second, ".sailune/sessions") {
		t.Fatal("old syncable default reused")
	}
}
