package sailune

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestScraperGuestAndAuthenticatedBothSites(t *testing.T) {
	for _, site := range []Site{AO3, FFN} {
		for _, authenticated := range []bool{false, true} {
			t.Run(string(site)+map[bool]string{false: "/guest", true: "/authenticated"}[authenticated], func(t *testing.T) {
				sessions := SessionStore{Dir: t.TempDir()}
				host, _ := siteHost(site)
				if authenticated {
					if _, err := sessions.Import(site, strings.NewReader(cookieExport(host, "session", "TEST_LOGIN"))); err != nil {
						t.Fatal(err)
					}
				}
				page := fixture(t, site)
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host != host || req.URL.Scheme != "https" {
						t.Fatalf("unexpected target %s", req.URL)
					}
					if req.Header.Get("User-Agent") != "TestBrowser/1" {
						t.Fatal("user agent missing")
					}
					if site == AO3 && req.URL.Query().Get("view_adult") != "true" {
						t.Fatal("missing adult confirmation")
					}
					cookie, err := req.Cookie("session")
					if authenticated && (err != nil || cookie.Value != "TEST_LOGIN") {
						t.Fatal("saved login not sent")
					}
					if !authenticated && err == nil {
						t.Fatal("guest request included a login")
					}
					resp := response(req, 200, page)
					if authenticated {
						resp.Header.Add("Set-Cookie", "session=ROTATED; Path=/; Secure; HttpOnly")
					}
					return resp, nil
				})}
				s := &Scraper{Sessions: sessions, Client: client, UserAgent: "TestBrowser/1"}
				u := "https://" + host + "/works/123"
				if site == FFN {
					u = "https://m.fanfiction.net/s/123/4/Example"
				}
				m, err := s.Fetch(context.Background(), u)
				if err != nil || m.Title == "" || m.FetchedAt.IsZero() {
					t.Fatalf("fetch: %+v %v", m, err)
				}
				status, err := sessions.Status(site)
				if err != nil || status.Configured != authenticated {
					t.Fatalf("session state: %+v %v", status, err)
				}
				if authenticated {
					path, _ := sessions.path(site)
					j, _, err := loadSession(path, site)
					if err != nil {
						t.Fatal(err)
					}
					cookies := j.snapshot()
					if len(cookies) != 1 || cookies[0].Cookie.Value != "ROTATED" {
						t.Fatal("response cookie not persisted")
					}
				}
			})
		}
	}
}

func TestScraperHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		page   string
		want   error
	}{
		{401, "", ErrLoginRequired}, {403, "", ErrChallenge}, {404, "", ErrWorkUnavailable},
		{410, "", ErrWorkUnavailable}, {429, "", ErrRateLimited},
		{200, `<title>Just a moment...</title>`, ErrChallenge},
		{200, `<title>Log In</title>`, ErrLoginRequired},
	} {
		s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return response(req, tc.status, tc.page), nil })}}
		if _, err := s.Fetch(context.Background(), "https://archiveofourown.org/works/123"); !errors.Is(err, tc.want) {
			t.Fatalf("HTTP %d: %v, want %v", tc.status, err, tc.want)
		}
	}
	for _, mode := range []string{"oversize", "non-html", "server-error"} {
		s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			resp := response(req, 200, "")
			if mode == "oversize" {
				resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 8<<20+1)))
			}
			if mode == "non-html" {
				resp.Header.Set("Content-Type", "application/json")
			}
			if mode == "server-error" {
				resp.StatusCode = 503
			}
			return resp, nil
		})}}
		if _, err := s.Fetch(context.Background(), "https://fanfiction.net/s/123/1"); err == nil {
			t.Fatalf("accepted %s", mode)
		}
	}
}

func TestScraperRedirectIsolationAndCancellation(t *testing.T) {
	for _, target := range []string{"https://example.com/steal?token=SECRET", "https://www.fanfiction.net/s/1/1", "http://archiveofourown.org/works/123"} {
		calls := 0
		s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			resp := response(req, 302, "")
			resp.Header.Set("Location", target)
			return resp, nil
		})}}
		_, err := s.Fetch(context.Background(), "https://archiveofourown.org/works/123")
		if err == nil || calls != 1 || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("unsafe redirect: calls=%d err=%v", calls, err)
		}
	}
	calls := 0
	s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path == "/users/login" {
			return response(req, 200, `<title>Log in</title>`), nil
		}
		resp := response(req, 302, "")
		resp.Header.Set("Location", "/users/login?restricted=true")
		return resp, nil
	})}}
	if _, err := s.Fetch(context.Background(), "https://archiveofourown.org/works/123"); !errors.Is(err, ErrLoginRequired) || calls != 2 {
		t.Fatalf("login redirect: %v calls=%d", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() })
	if _, err := s.Fetch(ctx, "https://archiveofourown.org/works/123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
