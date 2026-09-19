package scrape

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestRetryRecoveryAndPermanentFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		statuses  []int
		challenge bool
		want      int
	}{
		{"origin TLS recovers", []int{525, 525, 200}, false, 3},
		{"gateway recovers", []int{502, 503, 200}, false, 3},
		{"persistent failure capped", []int{525}, false, 4},
		{"challenge never retried", []int{503}, true, 1},
		{"forbidden", []int{403}, false, 1},
		{"login", []int{401}, false, 1},
		{"missing", []int{404}, false, 1},
		{"rate limit without deadline", []int{429}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				status := tc.statuses[min(calls, len(tc.statuses)-1)]
				calls++
				resp := response(r, status, "")
				if tc.challenge {
					resp.Header.Set("Cf-Mitigated", "challenge")
				}
				return resp, nil
			})}
			req, _ := http.NewRequest("GET", "https://archiveofourown.org/works/123", nil)
			resp, err := retryRequests(context.Background(), c, req, func(context.Context, time.Duration) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if calls != tc.want {
				t.Fatalf("calls=%d want=%d", calls, tc.want)
			}
		})
	}
}

func TestRetryAfterBudgetAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		cancel       bool
		wantCalls    int
	}{{"within budget", "2", false, 2}, {"beyond budget", "120", false, 1}, {"cancel backoff", "2", true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				resp := response(r, 200, "")
				if calls == 1 {
					resp.StatusCode = 429
					resp.Header.Set("Retry-After", tc.header)
				}
				return resp, nil
			})}
			req, _ := http.NewRequest("GET", "https://archiveofourown.org/works/123", nil)
			resp, err := retryRequests(ctx, client, req, func(ctx context.Context, d time.Duration) error {
				if d < 2*time.Second {
					t.Fatal("ignored retry-after")
				}
				if tc.cancel {
					cancel()
					return waitForRetry(ctx, d)
				}
				return nil
			})
			if resp != nil {
				resp.Body.Close()
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
func TestRetryAfterDate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	d, ok := retryAfter(now.Add(5*time.Second).Format(http.TimeFormat), now)
	if !ok || d != 5*time.Second {
		t.Fatal(d, ok)
	}
}

func TestFetchRecovers525AndParsesMetadata(t *testing.T) {
	calls := 0
	s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(r, 525, "SSL handshake failed"), nil
		}
		return response(r, 200, fixture(t, AO3)), nil
	})}}
	m, err := s.Fetch(context.Background(), "https://archiveofourown.org/works/123")
	if err != nil || m.Title == "" || m.FetchedAt.IsZero() || calls != 2 {
		t.Fatalf("metadata=%+v calls=%d err=%v", m, calls, err)
	}
}

func TestFetchDeadlineStopsBackoff(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Scraper{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; cancel(); return response(r, 525, ""), nil })}}
	_, err := s.Fetch(ctx, "https://archiveofourown.org/works/123")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestRetryTransportFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		want    int
	}{{"dropped connection", io.EOF, 2}, {"TLS or redirect failure", errors.New("refused redirect"), 1}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, tc.failure
				}
				return response(r, 200, ""), nil
			})}
			req, _ := http.NewRequest("GET", "https://archiveofourown.org/works/123", nil)
			resp, _ := retryRequests(context.Background(), client, req, func(context.Context, time.Duration) error { return nil })
			if resp != nil {
				resp.Body.Close()
			}
			if calls != tc.want {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
