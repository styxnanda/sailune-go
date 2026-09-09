package auth

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func cookieExport(domain, name, value string) string {
	return "# Netscape HTTP Cookie File\n#HttpOnly_" + domain + "\tTRUE\t/\tTRUE\t0\t" + name + "\t" + value + "\n"
}

func TestSessionImportScopeAndExpiry(t *testing.T) {
	store := SessionStore{Dir: filepath.Join(t.TempDir(), "sessions")}
	for _, tc := range []struct {
		site         Site
		domain, host string
	}{{AO3, ".archiveofourown.org", "archiveofourown.org"}, {FFN, ".fanfiction.net", "www.fanfiction.net"}} {
		t.Run(string(tc.site), func(t *testing.T) {
			input := cookieExport(tc.domain, "session", "TEST_SECRET") + ".example.com\tTRUE\t/\tTRUE\t0\tunrelated\tOTHER_SECRET\n" + tc.domain + "\tTRUE\t/\tTRUE\t1\texpired\tOLD_SECRET\n"
			status, err := store.Import(tc.site, strings.NewReader(input))
			if err != nil || !status.Configured || status.UsableCookies != 1 {
				t.Fatalf("import: %+v %v", status, err)
			}
			path, _ := store.path(tc.site)
			data, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(data), "OTHER_SECRET") || strings.Contains(string(data), "OLD_SECRET") {
				t.Fatalf("scope: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatalf("permissions: %v", info.Mode())
			}
			j, _, err := loadSession(path, tc.site)
			if err != nil {
				t.Fatal(err)
			}
			for _, u := range []string{"https://example.com/", "http://" + tc.host + "/", "https://" + tc.host + ".example.com/"} {
				parsed, _ := url.Parse(u)
				if len(j.Cookies(parsed)) != 0 {
					t.Fatal("cookies escaped their HTTPS site scope")
				}
			}
			if _, err := store.Import(tc.site, strings.NewReader("BAD_SECRET_DATA")); err == nil || strings.Contains(err.Error(), "BAD_SECRET_DATA") {
				t.Fatalf("invalid import: %v", err)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(data) {
				t.Fatal("invalid import replaced session")
			}
			if err := store.Clear(tc.site); err != nil {
				t.Fatal(err)
			}
			status, err = store.Status(tc.site)
			if err != nil || status.Configured {
				t.Fatalf("clear: %+v %v", status, err)
			}
		})
	}
}

func TestCookieRotationAndMatching(t *testing.T) {
	store := SessionStore{Dir: t.TempDir()}
	input := "archiveofourown.org\tFALSE\t/works\tTRUE\t0\thost_only\tA\n" + cookieExport(".archiveofourown.org", "session", "OLD")
	if _, err := store.Import(AO3, strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	err := store.WithJar(AO3, func(jar http.CookieJar) error {
		u, _ := url.Parse("https://archiveofourown.org/works/1")
		if len(jar.Cookies(u)) != 2 {
			t.Fatal("cookies not sent to matching path")
		}
		sub, _ := url.Parse("https://www.archiveofourown.org/works/1")
		if len(jar.Cookies(sub)) != 1 {
			t.Fatal("host-only cookie sent to subdomain")
		}
		jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "NEW", Domain: "archiveofourown.org", Path: "/", Secure: true, MaxAge: 3600}})
		jar.SetCookies(u, []*http.Cookie{{Name: "host_only", Path: "/works", MaxAge: -1}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.path(AO3)
	j, _, err := loadSession(path, AO3)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://archiveofourown.org/works/1")
	cookies := j.Cookies(u)
	if len(cookies) != 1 || cookies[0].Value != "NEW" {
		t.Fatalf("rotation/deletion not persisted: %d cookies", len(cookies))
	}
	for _, c := range j.snapshot() {
		if c.Cookie.MaxAge != 0 || !c.Cookie.Expires.After(time.Now()) {
			t.Fatal("relative expiry was not persisted as absolute time")
		}
	}
}

func TestSessionValidationAndLocks(t *testing.T) {
	s := SessionStore{Dir: t.TempDir()}
	for _, input := range []string{
		".archiveofourown.org\tTRUE\t/\tTRUE\t1\told\tEXPIRED\n",
		cookieExport(".fanfiction.net", "session", "WRONG_SITE"),
		".archiveofourown.org\tTRUE\t/\tTRUE\t0\tbad name\tSECRET\n",
		".archiveofourown.org\tTRUE\t/\tTRUE\tnan\tname\tSECRET\n",
	} {
		if _, err := s.Import(AO3, strings.NewReader(input)); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("invalid cookie input: %v", err)
		}
	}
	path, _ := s.path(AO3)
	if err := os.WriteFile(path, []byte(`{"version":1,"cookies":[{"cookie":{"Value":"PRIVATE"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(AO3); err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("corrupt session: %v", err)
	}
	if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(AO3); err == nil {
		t.Fatal("clear ignored writer lock")
	}
}
