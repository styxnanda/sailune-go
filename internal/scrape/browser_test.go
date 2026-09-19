package scrape

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type browserFunc func(context.Context, string) (string, string, error)

func (f browserFunc) Load(c context.Context, u string) (string, string, error) { return f(c, u) }
func TestBrowserRecoveryValidation(t *testing.T) {
	for _, tc := range []struct {
		name, url, body string
		ok              bool
	}{
		{"valid", "https://www.fanfiction.net/s/123/1", fixture(t, FFN), true},
		{"wrong work", "https://www.fanfiction.net/s/456/1", fixture(t, FFN), false},
		{"wrong origin", "https://example.com/s/123/1", fixture(t, FFN), false},
		{"insecure", "http://www.fanfiction.net/s/123/1", fixture(t, FFN), false},
		{"challenge", "https://www.fanfiction.net/s/123/1", "<title>Just a moment...</title>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &BrowserRecovery{Loader: browserFunc(func(context.Context, string) (string, string, error) { return tc.url, tc.body, nil })}
			m, e := b.fetch(context.Background(), "https://www.fanfiction.net/s/123/1")
			if (e == nil) != tc.ok {
				t.Fatal(e)
			}
			if tc.ok && m.FetchedAt.IsZero() {
				t.Fatal("missing timestamp")
			}
		})
	}
}
func TestBrowserFallbackRouting(t *testing.T) {
	for _, tc := range []struct {
		status int
		site   Site
		want   int
	}{{403, FFN, 1}, {404, FFN, 0}, {429, FFN, 0}, {401, FFN, 0}, {403, AO3, 0}, {200, FFN, 0}} {
		calls := 0
		s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(r, tc.status, fixture(t, tc.site)), nil })}, Browser: &BrowserRecovery{Loader: browserFunc(func(_ context.Context, u string) (string, string, error) { calls++; return u, fixture(t, FFN), nil })}}
		url := "https://www.fanfiction.net/s/123/1"
		if tc.site == AO3 {
			url = "https://archiveofourown.org/works/123"
		}
		_, _ = s.Fetch(context.Background(), url)
		if calls != tc.want {
			t.Fatalf("status=%d site=%s calls=%d", tc.status, tc.site, calls)
		}
	}
}
func TestBrowserCooldownAndBusy(t *testing.T) {
	calls := 0
	b := &BrowserRecovery{Loader: browserFunc(func(context.Context, string) (string, string, error) { calls++; return "", "", errors.New("blocked") })}
	for range 2 {
		_, _ = b.fetch(context.Background(), "https://www.fanfiction.net/s/123/1")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	b.retryAt = time.Time{}
	b.busy = true
	_, _ = b.fetch(context.Background(), "https://www.fanfiction.net/s/123/1")
	if calls != 1 {
		t.Fatal("concurrent browser launched")
	}
}
func TestBrowserCancellationRejectsLatePage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &BrowserRecovery{Loader: browserFunc(func(_ context.Context, u string) (string, string, error) { cancel(); return u, fixture(t, FFN), nil })}
	_, err := b.fetch(ctx, "https://www.fanfiction.net/s/123/1")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !b.retryAt.IsZero() {
		t.Fatal("user cancellation should not start cooldown")
	}
}

func TestBrowserDeadlineStartsCooldown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	b := &BrowserRecovery{Loader: browserFunc(func(ctx context.Context, _ string) (string, string, error) { <-ctx.Done(); return "", "", ctx.Err() })}
	_, err := b.fetch(ctx, "https://www.fanfiction.net/s/123/1")
	if !errors.Is(err, context.DeadlineExceeded) || b.retryAt.IsZero() {
		t.Fatalf("deadline=%v cooldown=%v", err, b.retryAt)
	}
}
