package auth

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/styxnanda/sailune-go/internal/model"
)

// SessionStore keeps imported browser sessions outside the bookmark database.
// Cookies are encrypted with OS-held keys; status never returns values.
type SessionStore struct{ Dir string }

type SessionStatus struct {
	Site          Site `json:"site"`
	Configured    bool `json:"configured"`
	UsableCookies int  `json:"usable_cookies"`
}

type savedCookie struct {
	Origin string      `json:"origin"`
	Cookie http.Cookie `json:"cookie"`
}

type sessionFile struct {
	Version int           `json:"version"`
	Cookies []savedCookie `json:"cookies"`
}

func (s SessionStore) path(site Site) (string, error) {
	if _, err := model.SiteHost(site); err != nil {
		return "", err
	}
	if s.Dir == "" {
		return "", errors.New("session directory is required")
	}
	return filepath.Join(s.Dir, string(site)+".json"), nil
}

// Import reads a Netscape cookies.txt export, retaining only the requested site.
// A valid import replaces the site's previous session; bad imports leave it intact.
func (s SessionStore) Import(site Site, r io.Reader) (SessionStatus, error) {
	if _, err := model.SiteHost(site); err != nil {
		return SessionStatus{}, err
	}
	j := newSessionJar(site)
	scanner := bufio.NewScanner(io.LimitReader(r, 4<<20+1))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	line, size := 0, 0
	for scanner.Scan() {
		line++
		text := strings.TrimSuffix(scanner.Text(), "\r")
		size += len(text) + 1
		if size > 4<<20 {
			return SessionStatus{}, errors.New("cookie file exceeds 4 MiB")
		}
		httpOnly := strings.HasPrefix(text, "#HttpOnly_")
		if httpOnly {
			text = strings.TrimPrefix(text, "#HttpOnly_")
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		parts := strings.Split(text, "\t")
		if len(parts) != 7 {
			return SessionStatus{}, fmt.Errorf("invalid Netscape cookie format at line %d (expected 7 tab-separated fields)", line)
		}
		if !model.SiteDomain(site, parts[0]) {
			continue
		}
		expires, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil || expires < 0 || (parts[1] != "TRUE" && parts[1] != "FALSE") || (parts[3] != "TRUE" && parts[3] != "FALSE") || !strings.HasPrefix(parts[2], "/") {
			return SessionStatus{}, fmt.Errorf("invalid cookie attributes at line %d", line)
		}
		c := &http.Cookie{Name: parts[5], Value: parts[6], Path: parts[2], Secure: parts[3] == "TRUE", HttpOnly: httpOnly}
		domain := strings.TrimPrefix(strings.ToLower(parts[0]), ".")
		if parts[1] == "TRUE" {
			c.Domain = domain
		}
		if expires > 0 {
			c.Expires = time.Unix(expires, 0)
		}
		if c.Valid() != nil {
			return SessionStatus{}, fmt.Errorf("invalid cookie at line %d", line)
		}
		j.SetCookies(&url.URL{Scheme: "https", Host: domain, Path: "/"}, []*http.Cookie{c})
	}
	if scanner.Err() != nil {
		return SessionStatus{}, errors.New("could not read cookie file (expected Netscape cookies.txt format)")
	}
	if len(j.snapshot()) == 0 {
		return SessionStatus{}, errors.New("cookie file contains no unexpired cookies for this site")
	}
	err := s.locked(site, func(path string) error { return writePrivateJSON(path, sessionFile{Version: 1, Cookies: j.snapshot()}) })
	if err != nil {
		return SessionStatus{}, err
	}
	return s.Status(site)
}

func (s SessionStore) Status(site Site) (SessionStatus, error) {
	path, err := s.path(site)
	if err != nil {
		return SessionStatus{}, err
	}
	j, configured, err := loadSession(path, site)
	if err != nil {
		return SessionStatus{}, err
	}
	host, _ := model.SiteHost(site)
	return SessionStatus{Site: site, Configured: configured, UsableCookies: len(j.Cookies(&url.URL{Scheme: "https", Host: host, Path: "/"}))}, nil
}

func (s SessionStore) Clear(site Site) error {
	return s.locked(site, func(path string) error {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

// WithJar holds a per-site lock across load/fetch/save so cookie rotation cannot race an
// import, clear, or another fetch. No bookmark lock is held during network I/O.
func (s SessionStore) WithJar(site Site, fn func(http.CookieJar) error) error {
	if s.Dir == "" {
		return fn(newSessionJar(site))
	}
	return s.locked(site, func(path string) error {
		j, configured, err := loadSession(path, site)
		if err != nil {
			return err
		}
		fetchErr := fn(j)
		if configured {
			if err := writePrivateJSON(path, sessionFile{Version: 1, Cookies: j.snapshot()}); err != nil {
				return errors.Join(fetchErr, fmt.Errorf("save session: %w", err))
			}
		}
		return fetchErr
	})
}

func (s SessionStore) locked(site Site, fn func(string) error) error {
	path, err := s.path(site)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return errors.New("site session is busy; retry later (after a crash, remove its .lock file only when no Sailune process is running)")
	}
	if err != nil {
		return err
	}
	f.Close()
	defer os.Remove(path + ".lock")
	return fn(path)
}

func loadSession(path string, site Site) (*sessionJar, bool, error) {
	j := newSessionJar(site)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxEncryptedSession+1))
	if err != nil {
		return nil, false, err
	}
	plain, err := openSession(data, site)
	if err != nil {
		return nil, false, err
	}
	defer clear(plain)
	return decodeSession(plain, site)
}

func decodeSession(data []byte, site Site) (*sessionJar, bool, error) {
	j := newSessionJar(site)
	var saved sessionFile
	if len(data) > 4<<20 || json.Unmarshal(data, &saved) != nil || saved.Version != 1 || saved.Cookies == nil {
		return nil, false, errors.New("invalid session data; re-import cookies with auth (file left untouched)")
	}
	for _, c := range saved.Cookies {
		u, err := url.Parse(c.Origin)
		if err != nil || u.Scheme != "https" || u.User != nil || !model.SiteDomain(site, u.Host) || c.Cookie.Valid() != nil || (c.Cookie.Domain != "" && !model.SiteDomain(site, c.Cookie.Domain)) {
			return nil, false, errors.New("invalid session cookie; re-import cookies with auth")
		}
		j.SetCookies(u, []*http.Cookie{&c.Cookie})
	}
	return j, true, nil
}

func writePrivateJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > 4<<20 {
		return errors.New("session exceeds 4 MiB; export only this site's login cookies")
	}
	defer clear(data)
	data, err = sealSession(path, sessionSite(path), data)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sailune-session-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Track cookie attributes alongside net/http's RFC-compliant matching jar.
type sessionJar struct {
	mu      sync.Mutex
	jar     *cookiejar.Jar
	site    Site
	records map[string]savedCookie
}

func newSessionJar(site Site) *sessionJar {
	j, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &sessionJar{jar: j, site: site, records: map[string]savedCookie{}}
}

func (j *sessionJar) Cookies(u *url.URL) []*http.Cookie {
	if u.Scheme != "https" || !model.SiteDomain(j.site, u.Host) {
		return nil
	}
	return j.jar.Cookies(u)
}

func (j *sessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if u.Scheme != "https" || !model.SiteDomain(j.site, u.Host) {
		return
	}
	for _, original := range cookies {
		c := *original
		domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if domain == "" {
			domain = u.Hostname()
		}
		if !model.SiteDomain(j.site, domain) || (u.Hostname() != domain && !strings.HasSuffix(u.Hostname(), "."+domain)) || c.Valid() != nil {
			continue
		}
		if !strings.HasPrefix(c.Path, "/") {
			c.Path = u.Path[:strings.LastIndex(u.Path, "/")+1]
			c.Path = strings.TrimSuffix(c.Path, "/")
			if c.Path == "" {
				c.Path = "/"
			}
		}
		key := domain + "\n" + c.Path + "\n" + c.Name
		j.jar.SetCookies(u, []*http.Cookie{&c})
		if c.MaxAge < 0 || (c.MaxAge == 0 && !c.Expires.IsZero() && !c.Expires.After(time.Now())) {
			delete(j.records, key)
			continue
		}
		if c.MaxAge > 0 {
			c.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
			c.MaxAge = 0
		}
		j.records[key] = savedCookie{Origin: "https://" + u.Hostname() + "/", Cookie: c}
	}
}

func (j *sessionJar) snapshot() []savedCookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	result := []savedCookie{}
	for _, c := range j.records {
		if c.Cookie.Expires.IsZero() || c.Cookie.Expires.After(time.Now()) {
			result = append(result, c)
		}
	}
	return result
}
