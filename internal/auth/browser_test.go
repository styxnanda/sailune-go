package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/styxnanda/sailune-go/internal/model"
)

func browserFixture(t *testing.T, browser string, version int) (string, *sql.DB) {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "profile")
	name := "cookies.sqlite"
	if browser != "firefox" {
		name = "Network/Cookies"
	}
	path := filepath.Join(profile, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	execSQL(t, db, "PRAGMA journal_mode=WAL")
	if browser == "firefox" {
		execSQL(t, db, "PRAGMA user_version = "+strconv.Itoa(version))
		execSQL(t, db, "CREATE TABLE moz_cookies (host TEXT,name TEXT,value TEXT,path TEXT,expiry INTEGER,isSecure INTEGER,isHttpOnly INTEGER,originAttributes TEXT)")
	} else {
		execSQL(t, db, "CREATE TABLE meta (key TEXT,value INTEGER)")
		execSQL(t, db, "INSERT INTO meta VALUES ('version',?)", version)
		execSQL(t, db, "CREATE TABLE cookies (host_key TEXT,name TEXT,value TEXT,encrypted_value BLOB,path TEXT,expires_utc INTEGER,is_secure INTEGER,is_httponly INTEGER,top_frame_site_key TEXT)")
	}
	return profile, db
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestFirefoxBrowserImportBothSites(t *testing.T) {
	for _, version := range []int{15, 16, 17} {
		profile, db := browserFixture(t, "firefox", version)
		expiry := time.Now().Add(time.Hour).Unix()
		if version >= 16 {
			expiry *= 1000
		}
		for _, host := range []string{".archiveofourown.org", ".fanfiction.net", ".unrelated.example"} {
			execSQL(t, db, "INSERT INTO moz_cookies VALUES (?, 'login', 'TEST_SESSION', '/', ?, 1, 1, '')", host, expiry)
		}
		execSQL(t, db, "INSERT INTO moz_cookies VALUES ('.archiveofourown.org','container','CONTAINER_SECRET','/',?,1,1,'^userContextId=1')", expiry)
		execSQL(t, db, "INSERT INTO moz_cookies VALUES ('.archiveofourown.org','expired','OLD','/',1,1,1,'')")
		for _, site := range []Site{AO3, FFN} {
			s := SessionStore{Dir: t.TempDir()}
			status, err := s.ImportBrowser(context.Background(), site, "firefox:"+profile)
			if err != nil || status.UsableCookies != 1 {
				t.Fatalf("Firefox %d %s: %+v %v", version, site, status, err)
			}
			path, _ := s.path(site)
			data, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(data), "unrelated") || strings.Contains(string(data), "CONTAINER_SECRET") {
				t.Fatal("import escaped site/container scope")
			}
			jar, _, err := loadSession(path, site)
			if err != nil {
				t.Fatal(err)
			}
			host, _ := model.SiteHost(site)
			cookies := jar.Cookies(&url.URL{Scheme: "https", Host: host, Path: "/"})
			if len(cookies) != 1 || cookies[0].Value != "TEST_SESSION" {
				t.Fatal("imported login unavailable")
			}
		}
		var count int
		if err := db.QueryRow("SELECT count(*) FROM moz_cookies").Scan(&count); err != nil || count != 5 {
			t.Fatal("browser database was changed")
		}
	}
}

func TestChromiumFiltersBeforeDecryption(t *testing.T) {
	profile, db := browserFixture(t, "brave", 24)
	future := (time.Now().Add(time.Hour).Unix() + 11644473600) * 1000000
	for _, host := range []string{".archiveofourown.org", ".fanfiction.net", ".other.example"} {
		execSQL(t, db, "INSERT INTO cookies VALUES (?, 'login', '', ?, '/', ?, 1, 1, '')", host, []byte("v10encrypted"), future)
	}
	execSQL(t, db, "INSERT INTO cookies VALUES ('.archiveofourown.org','partitioned','',?,'/',?,1,1,'https://other.example')", []byte("v20bad"), future)
	execSQL(t, db, "INSERT INTO cookies VALUES ('.archiveofourown.org','expired','',?,'/',1,1,1,'')", []byte("v20bad"))
	execSQL(t, db, "INSERT INTO cookies VALUES ('archiveofourown.org','hostonly','plain',NULL,'/works',0,1,1,'')")
	for _, site := range []Site{AO3, FFN} {
		calls := 0
		jar, err := readBrowserCookies(context.Background(), site, "brave", profile, func(host string, value []byte, version int) (string, error) {
			calls++
			if !model.SiteDomain(site, host) || version != 24 || string(value) != "v10encrypted" {
				t.Fatal("unexpected cookie decrypted")
			}
			return "DECRYPTED", nil
		})
		if err != nil || calls != 1 {
			t.Fatalf("%s: calls=%d %v", site, calls, err)
		}
		if site == AO3 {
			cookies := jar.Cookies(&url.URL{Scheme: "https", Host: "archiveofourown.org", Path: "/works/1"})
			if len(cookies) != 2 {
				t.Fatal("host-only/path cookie not preserved")
			}
			if len(jar.Cookies(&url.URL{Scheme: "https", Host: "www.archiveofourown.org", Path: "/works/1"})) != 1 {
				t.Fatal("host-only cookie scope widened")
			}
		}
	}
}

func TestBrowserImportFailurePreservesSession(t *testing.T) {
	profile, db := browserFixture(t, "brave", 24)
	execSQL(t, db, "INSERT INTO cookies VALUES ('.archiveofourown.org','login','',?,'/',0,1,1,'')", []byte("v20TEST_SECRET"))
	s := SessionStore{Dir: t.TempDir()}
	if _, err := s.Import(AO3, strings.NewReader(cookieExport(".archiveofourown.org", "login", "EXISTING"))); err != nil {
		t.Fatal(err)
	}
	path, _ := s.path(AO3)
	before, _ := os.ReadFile(path)
	_, err := s.ImportBrowser(context.Background(), AO3, "brave:"+profile)
	if !errors.Is(err, errAppBound) || strings.Contains(err.Error(), "TEST_SECRET") {
		t.Fatalf("v20: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed browser import replaced session")
	}
}

func TestBrowserProfileSelection(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Default", "Profile 1"} {
		if err := os.MkdirAll(filepath.Join(root, name, "Network"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "Network/Cookies"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := chooseBrowserProfile(BrowserSpec{Name: "brave"}, []string{root}); err == nil {
		t.Fatal("ambiguous profile auto-selected")
	}
	p, err := chooseBrowserProfile(BrowserSpec{Name: "brave", Profile: "Profile 1"}, []string{root})
	if err != nil || p != filepath.Join(root, "Profile 1") {
		t.Fatalf("profile: %s %v", p, err)
	}
	for _, value := range []string{"brave", "firefox:/tmp/profile", "chrome:Profile 1", `edge:C:\Users\test\Profile 1`} {
		if _, err := ParseBrowserSpec(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"", "safari", "brave:"} {
		if _, err := ParseBrowserSpec(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if got := browserRoots("brave", "darwin", "/home/test", "", "", "")[0]; !strings.HasSuffix(filepath.ToSlash(got), "BraveSoftware/Brave-Browser") {
		t.Fatal(got)
	}
}

func TestChromiumCryptoAndHostBinding(t *testing.T) {
	for _, rounds := range []int{1, 1003} {
		key, err := pbkdf2.Key(sha1.New, "test-key", []byte("saltysalt"), rounds, 16)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(".archiveofourown.org"))
		plain := append(hash[:], []byte("COOKIE_VALUE")...)
		pad := aes.BlockSize - len(plain)%aes.BlockSize
		padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
		block, _ := aes.NewCipher(key)
		encrypted := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, 16)).CryptBlocks(encrypted, padded)
		decrypted, err := decryptCBC(key, encrypted)
		if err != nil || !bytes.Equal(decrypted, plain) {
			t.Fatal("CBC round trip failed")
		}
		value, err := chromiumPlaintext(".archiveofourown.org", decrypted, 24)
		if err != nil || value != "COOKIE_VALUE" {
			t.Fatal("host hash not decoded")
		}
		if _, err := chromiumPlaintext(".fanfiction.net", decrypted, 24); err == nil {
			t.Fatal("host binding ignored")
		}
		if _, err := decryptCBC(key, encrypted[:len(encrypted)-1]); err == nil {
			t.Fatal("invalid CBC length accepted")
		}
		gcm, _ := cipher.NewGCM(block)
		nonce := bytes.Repeat([]byte{1}, gcm.NonceSize())
		payload := append(append([]byte(nil), nonce...), gcm.Seal(nil, nonce, plain, nil)...)
		decrypted, err = decryptGCM(key, payload)
		if err != nil || !bytes.Equal(decrypted, plain) {
			t.Fatal("GCM round trip failed")
		}
		payload[len(payload)-1] ^= 1
		if _, err := decryptGCM(key, payload); err == nil {
			t.Fatal("GCM authentication ignored")
		}
	}
}

func TestChromiumPlatformKeySelection(t *testing.T) {
	host := ".fanfiction.net"
	hash := sha256.Sum256([]byte(host))
	plain := append(hash[:], []byte("LOGIN_VALUE")...)
	for _, tc := range []struct {
		platform, prefix, password string
		rounds                     int
	}{
		{"darwin", "v10", "KEYCHAIN_SECRET", 1003},
		{"linux", "v10", "peanuts", 1},
		{"linux", "v11", "KEYRING_SECRET", 1},
	} {
		key, _ := pbkdf2.Key(sha1.New, tc.password, []byte("saltysalt"), tc.rounds, 16)
		block, _ := aes.NewCipher(key)
		pad := aes.BlockSize - len(plain)%aes.BlockSize
		padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
		encrypted := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, 16)).CryptBlocks(encrypted, padded)
		calls := 0
		decrypt := newChromiumDecryptor(tc.platform, func() ([]byte, error) { calls++; return []byte(tc.password), nil }, nil, nil)
		for i := 0; i < 2; i++ {
			value, err := decrypt(host, append([]byte(tc.prefix), encrypted...), 24)
			if err != nil || value != "LOGIN_VALUE" {
				t.Fatalf("%s/%s: %v", tc.platform, tc.prefix, err)
			}
		}
		wantCalls := 1
		if tc.platform == "linux" && tc.prefix == "v10" {
			wantCalls = 0
		}
		if calls != wantCalls {
			t.Fatalf("key lookup count: %d, want %d", calls, wantCalls)
		}
	}
	key := bytes.Repeat([]byte{7}, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := bytes.Repeat([]byte{2}, gcm.NonceSize())
	payload := append([]byte("v10"), nonce...)
	payload = append(payload, gcm.Seal(nil, nonce, plain, nil)...)
	decrypt := newChromiumDecryptor("windows", nil, func() ([]byte, error) { return key, nil }, func([]byte) ([]byte, error) { return []byte("LEGACY_VALUE"), nil })
	value, err := decrypt(host, payload, 24)
	if err != nil || value != "LOGIN_VALUE" {
		t.Fatalf("Windows GCM: %v", err)
	}
	value, err = decrypt(host, []byte{1, 2, 3}, 23)
	if err != nil || value != "LEGACY_VALUE" {
		t.Fatalf("Windows legacy: %v", err)
	}
}

func TestBrowserFamilies(t *testing.T) {
	for _, tc := range []struct{ input, name, profile string }{
		{"chromium/brave:Default", "brave", "Default"},
		{"chromium/chrome:Profile 1", "chrome", "Profile 1"},
		{"CHROMIUM/OPERA", "opera", ""},
		{"chromium", "chromium", ""},
		{"gecko", "firefox", ""},
		{"gecko/firefox:/fork/profile", "firefox", "/fork/profile"},
		{`gecko:C:\Users\test\profile`, "firefox", `C:\Users\test\profile`},
	} {
		spec, err := ParseBrowserSpec(tc.input)
		if err != nil || spec.Name != tc.name || spec.Profile != tc.profile {
			t.Fatalf("%q: %+v, %v", tc.input, spec, err)
		}
	}
	for _, value := range []string{"gecko/brave", "chromium/firefox", "chromium/gecko", "webkit/chrome", "chromium/", "gecko/firefox:"} {
		if _, err := ParseBrowserSpec(value); err == nil {
			t.Fatalf("accepted invalid family source %q", value)
		}
	}
}

func TestOperaRootProfile(t *testing.T) {
	profile, _ := browserFixture(t, "opera", 24)
	got, err := chooseBrowserProfile(BrowserSpec{Name: "opera"}, []string{profile})
	if err != nil || got != profile {
		t.Fatalf("root profile: %q, %v", got, err)
	}
	for platform, suffix := range map[string]string{
		"darwin":  "Library/Application Support/com.operasoftware.Opera",
		"linux":   "config/opera",
		"windows": "roaming/Opera Software/Opera Stable",
	} {
		roots := browserRoots("opera", platform, "/home", "/home/config", "/home/local", "/home/roaming")
		if len(roots) != 1 || roots[0] != filepath.Join("/home", suffix) {
			t.Fatalf("%s: %v", platform, roots)
		}
	}
}

func TestFamilyImportBothSites(t *testing.T) {
	for _, browser := range []string{"firefox", "opera"} {
		profile, db := browserFixture(t, browser, 17)
		for _, host := range []string{".fanfiction.net", ".archiveofourown.org"} {
			if browser == "firefox" {
				execSQL(t, db, "INSERT INTO moz_cookies VALUES (?, 'login', 'TEST', '/', 0, 1, 1, '')", host)
			} else {
				execSQL(t, db, "INSERT INTO cookies VALUES (?, 'login', 'TEST', NULL, '/', 0, 1, 1, '')", host)
			}
		}
		source := "chromium/opera:" + profile
		if browser == "firefox" {
			source = "gecko:" + profile
		}
		for _, site := range []Site{AO3, FFN} {
			status, err := (SessionStore{Dir: t.TempDir()}).ImportBrowser(context.Background(), site, source)
			if err != nil || status.UsableCookies != 1 {
				t.Fatalf("%s %s: %+v, %v", source, site, status, err)
			}
		}
	}
}
