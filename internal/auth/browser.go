package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/styxnanda/sailune-go/internal/model"
)

// BrowserSpec selects a browser and optionally a profile name or directory.
// The syntax is family/browser[:profile]; legacy browser[:profile] is accepted.
type BrowserSpec struct{ Name, Profile string }

func ParseBrowserSpec(value string) (BrowserSpec, error) {
	name, profile, hasProfile := strings.Cut(value, ":")
	name = strings.ToLower(name)
	if name == "gecko" {
		name = "firefox"
	}
	if family, browser, qualified := strings.Cut(name, "/"); qualified {
		if (family != "chromium" && family != "gecko") || browser == "" {
			return BrowserSpec{}, errors.New("use chromium/BROWSER[:PROFILE] or gecko/firefox[:PROFILE]")
		}
		if (family == "gecko") != (browser == "firefox") {
			return BrowserSpec{}, errors.New("browser does not belong to the selected family")
		}
		name = browser
	}
	switch name {
	case "brave", "chrome", "chromium", "edge", "opera", "vivaldi", "firefox":
	default:
		return BrowserSpec{}, errors.New("choose chromium/brave, chromium/chrome, chromium/chromium, chromium/edge, chromium/opera, chromium/vivaldi, or gecko/firefox")
	}
	if hasProfile && profile == "" {
		return BrowserSpec{}, errors.New("browser profile is empty; use BROWSER or BROWSER:PROFILE")
	}
	return BrowserSpec{Name: name, Profile: profile}, nil
}

// ImportBrowser reads only cookies belonging to site. It never logs cookie
// values or changes browser cookies. A failed read leaves saved sessions intact.
func (s SessionStore) ImportBrowser(ctx context.Context, site Site, value string) (SessionStatus, error) {
	if _, err := model.SiteHost(site); err != nil {
		return SessionStatus{}, err
	}
	spec, err := ParseBrowserSpec(value)
	if err != nil {
		return SessionStatus{}, err
	}
	profile, err := findBrowserProfile(spec)
	if err != nil {
		return SessionStatus{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	jar, err := readBrowserCookies(ctx, site, spec.Name, profile, nil)
	if err != nil {
		return SessionStatus{}, err
	}
	if len(jar.snapshot()) == 0 {
		return SessionStatus{}, errors.New("no unexpired cookies for this site in the selected browser profile; sign in there and select the correct profile")
	}
	err = s.locked(site, func(path string) error {
		return writePrivateJSON(path, sessionFile{Version: 1, Cookies: jar.snapshot()})
	})
	if err != nil {
		return SessionStatus{}, err
	}
	return s.Status(site)
}

func browserRoots(name, platform, home, config, local, roaming string) []string {
	if name == "firefox" {
		switch platform {
		case "darwin":
			return []string{filepath.Join(home, "Library/Application Support/Firefox/Profiles")}
		case "windows":
			return []string{filepath.Join(roaming, "Mozilla/Firefox/Profiles")}
		default:
			return []string{filepath.Join(config, "mozilla/firefox"), filepath.Join(home, ".mozilla/firefox"), filepath.Join(home, "snap/firefox/common/.mozilla/firefox"), filepath.Join(home, ".var/app/org.mozilla.firefox/config/mozilla/firefox"), filepath.Join(home, ".var/app/org.mozilla.firefox/.mozilla/firefox")}
		}
	}
	var base, suffix string
	switch platform {
	case "darwin":
		base = filepath.Join(home, "Library/Application Support")
		suffix = map[string]string{"brave": "BraveSoftware/Brave-Browser", "chrome": "Google/Chrome", "chromium": "Chromium", "edge": "Microsoft Edge", "opera": "com.operasoftware.Opera", "vivaldi": "Vivaldi"}[name]
	case "windows":
		base = local
		if name == "opera" {
			return []string{filepath.Join(roaming, "Opera Software/Opera Stable")}
		}
		suffix = map[string]string{"brave": "BraveSoftware/Brave-Browser/User Data", "chrome": "Google/Chrome/User Data", "chromium": "Chromium/User Data", "edge": "Microsoft/Edge/User Data", "vivaldi": "Vivaldi/User Data"}[name]
	default:
		base = config
		suffix = map[string]string{"brave": "BraveSoftware/Brave-Browser", "chrome": "google-chrome", "chromium": "chromium", "edge": "microsoft-edge", "opera": "opera", "vivaldi": "vivaldi"}[name]
	}
	return []string{filepath.Join(base, filepath.FromSlash(suffix))}
}

func findBrowserProfile(spec BrowserSpec) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if spec.Profile == "~" {
		spec.Profile = home
	} else if strings.HasPrefix(spec.Profile, "~/") {
		spec.Profile = filepath.Join(home, strings.TrimPrefix(spec.Profile, "~/"))
	}
	if filepath.IsAbs(spec.Profile) || strings.ContainsAny(spec.Profile, `/\`) {
		if cookieDB(spec.Name, spec.Profile) == "" {
			return "", errors.New("no cookie database in the specified profile directory")
		}
		return spec.Profile, nil
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	roots := browserRoots(spec.Name, runtime.GOOS, home, config, os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"))
	return chooseBrowserProfile(spec, roots)
}

func chooseBrowserProfile(spec BrowserSpec, roots []string) (string, error) {
	var candidates []string
	for _, root := range roots {
		if spec.Profile != "" {
			p := filepath.Join(root, spec.Profile)
			if cookieDB(spec.Name, p) != "" {
				candidates = append(candidates, p)
			}
			continue
		}
		// Opera can store cookies directly in its user-data directory.
		if spec.Name == "opera" && cookieDB(spec.Name, root) != "" {
			candidates = append(candidates, root)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if spec.Name != "firefox" && entry.Name() != "Default" && !strings.HasPrefix(entry.Name(), "Profile ") {
				continue
			}
			p := filepath.Join(root, entry.Name())
			if cookieDB(spec.Name, p) != "" {
				candidates = append(candidates, p)
			}
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("browser cookie database not found; open the browser and sign in, or use BROWSER:/absolute/profile/path")
	}
	if len(candidates) > 1 {
		sort.Strings(candidates)
		return "", fmt.Errorf("multiple browser profiles found; select one with BROWSER:PROFILE (profile directories: %s)", strings.Join(candidates, ", "))
	}
	return candidates[0], nil
}

func cookieDB(browser, profile string) string {
	names := []string{"Network/Cookies", "Cookies"}
	if browser == "firefox" {
		names = []string{"cookies.sqlite"}
	}
	for _, name := range names {
		p := filepath.Join(profile, filepath.FromSlash(name))
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

type cookieDecryptFunc func(host string, encrypted []byte, version int) (string, error)

func readBrowserCookies(ctx context.Context, site Site, browser, profile string, decrypt cookieDecryptFunc) (*sessionJar, error) {
	path := cookieDB(browser, profile)
	if path == "" {
		return nil, errors.New("browser cookie database not found")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if runtime.GOOS == "windows" {
		u.Path = "/" + u.Path
	}
	u.RawQuery = "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(2000)"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("cannot open browser cookies read-only")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// A read transaction gives a coherent view, including committed WAL data.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, browserDBError()
	}
	defer tx.Rollback()
	var version int
	if browser == "firefox" {
		err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	} else {
		err = tx.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'version'").Scan(&version)
	}
	if err != nil {
		return nil, browserDBError()
	}
	table := "cookies"
	if browser == "firefox" {
		table = "moz_cookies"
	}
	columns, err := browserColumns(ctx, tx, table)
	if err != nil {
		return nil, browserDBError()
	}
	query := "SELECT host_key, name, value, encrypted_value, path, expires_utc, is_secure, is_httponly FROM cookies WHERE host_key IN (?, ?, ?, ?, ?, ?)"
	if columns["top_frame_site_key"] {
		query += " AND top_frame_site_key = ''"
	}
	if browser == "firefox" {
		query = "SELECT host, name, value, NULL, path, expiry, isSecure, isHttpOnly FROM moz_cookies WHERE host IN (?, ?, ?, ?, ?, ?)"
		if columns["originAttributes"] {
			query += " AND originAttributes = ''"
		}
	}
	domains := []any{"archiveofourown.org", ".archiveofourown.org", "www.archiveofourown.org", ".www.archiveofourown.org", "", ""}
	if site == FFN {
		domains = []any{"fanfiction.net", ".fanfiction.net", "www.fanfiction.net", ".www.fanfiction.net", "m.fanfiction.net", ".m.fanfiction.net"}
	}
	rows, err := tx.QueryContext(ctx, query, domains...)
	if err != nil {
		return nil, browserDBError()
	}
	defer rows.Close()
	jar := newSessionJar(site)
	if decrypt == nil {
		decrypt = chromiumDecryptor(ctx, browser, profile)
	}
	for rows.Next() {
		var host, name, value, path string
		var encrypted []byte
		var expiry int64
		var secure, httpOnly bool
		if err := rows.Scan(&host, &name, &value, &encrypted, &path, &expiry, &secure, &httpOnly); err != nil {
			return nil, browserDBError()
		}
		if !model.SiteDomain(site, host) {
			continue
		}
		var expires time.Time
		if expiry > 0 {
			if browser == "firefox" {
				if version >= 16 {
					expires = time.UnixMilli(expiry)
				} else {
					expires = time.Unix(expiry, 0)
				}
			} else {
				expires = time.Unix(expiry/1000000-11644473600, 0)
			}
			if !expires.After(time.Now()) {
				continue
			}
		}
		if browser != "firefox" && len(encrypted) > 0 {
			value, err = decrypt(host, encrypted, version)
			if err != nil {
				return nil, err
			}
		}
		c := &http.Cookie{Name: name, Value: value, Path: path, Secure: secure, HttpOnly: httpOnly, Expires: expires}
		if strings.HasPrefix(host, ".") {
			c.Domain = host
		}
		if c.Valid() != nil {
			return nil, errors.New("browser contains an invalid site cookie; sign in again before importing")
		}
		jar.SetCookies(&url.URL{Scheme: "https", Host: strings.TrimPrefix(host, "."), Path: "/"}, []*http.Cookie{c})
	}
	if rows.Err() != nil {
		return nil, browserDBError()
	}
	return jar, nil
}

func browserColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var id, notnull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&id, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func browserDBError() error {
	return errors.New("cannot read browser cookie database (locked, inaccessible, or unsupported schema); close the browser and retry, or select another profile")
}
