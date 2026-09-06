package sailune

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrLoginRequired   = errors.New("login required or session expired; log in in your browser, then run sailune auth SITE --cookies FILE")
	ErrChallenge       = errors.New("site denied access or requires a browser challenge; open the work in your browser and refresh imported cookies; browser-bound challenges may still prevent fetching")
	ErrWorkUnavailable = errors.New("work is unavailable, deleted, or not visible to this account")
	ErrRateLimited     = errors.New("site rate limit reached; wait before retrying")
	ErrMetadata        = errors.New("story metadata was not found; the site layout may have changed or the work may be restricted")
)

// Scraper fetches a single work page with bounded I/O and optional sessions.
// Client can supply a custom transport for a GUI or tests; redirects, cookie
// scope, and timeout are still enforced. No automatic retries are performed.
type Scraper struct {
	Sessions  SessionStore
	Client    *http.Client
	UserAgent string
}

func (s *Scraper) Fetch(ctx context.Context, raw string) (Metadata, error) {
	canonical, site, _, err := NormalizeURL(raw)
	if err != nil {
		return Metadata{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result Metadata
	err = s.Sessions.withJar(site, func(jar http.CookieJar) error {
		client := http.Client{}
		if s.Client != nil {
			client = *s.Client
		}
		client.Jar = jar
		if client.Timeout <= 0 || client.Timeout > 30*time.Second {
			client.Timeout = 30 * time.Second
		}
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many site redirects")
			}
			if req.URL.Scheme != "https" || req.URL.User != nil || !siteDomain(site, req.URL.Host) {
				return errors.New("refused redirect outside the site's HTTPS hosts")
			}
			return nil
		}
		requestURL := canonical
		if site == AO3 {
			requestURL += "?view_adult=true"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return err
		}
		ua := s.UserAgent
		if ua == "" {
			ua = "Sailune/0.2 (+https://github.com/styxnanda/sailune-go)"
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		resp, err := client.Do(req)
		if err != nil {
			// Do not echo redirect URLs: they can contain login tokens.
			if ctx.Err() != nil {
				return fmt.Errorf("fetch interrupted: %w", ctx.Err())
			}
			return errors.New("could not fetch work: network, timeout, TLS, or disallowed redirect; try again or use --no-fetch")
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return ErrLoginRequired
		case http.StatusForbidden:
			return ErrChallenge
		case http.StatusNotFound, http.StatusGone:
			return ErrWorkUnavailable
		case http.StatusTooManyRequests:
			return ErrRateLimited
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("site returned HTTP %d; try again later", resp.StatusCode)
		}
		if strings.Contains(resp.Request.URL.Path, "login") {
			return ErrLoginRequired
		}
		if redirected, _, _, err := NormalizeURL(resp.Request.URL.String()); err == nil && redirected != canonical {
			return errors.New("site redirected to a different work; add that work's URL explicitly")
		}
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
			return errors.New("site returned a non-HTML response")
		}
		const maxPage = 8 << 20
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxPage+1))
		if err != nil {
			return errors.New("could not read work page")
		}
		if len(body) > maxPage {
			return errors.New("work page exceeds 8 MiB; use --no-fetch to bookmark it manually")
		}
		result, err = ParseMetadata(site, strings.NewReader(string(body)))
		if err == nil {
			result.FetchedAt = time.Now().UTC()
		}
		return err
	})
	if err != nil {
		return Metadata{}, fmt.Errorf("%s: %w", site, err)
	}
	return result, nil
}
