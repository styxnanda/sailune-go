package scrape

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Four attempts share Fetch's 30s budget (or the caller's shorter deadline).
// Never rotate hosts, weaken TLS, or replay authentication/challenge failures.
func doWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	return retryRequests(ctx, client, req, waitForRetry)
}

func retryRequests(ctx context.Context, client *http.Client, req *http.Request, wait func(context.Context, time.Duration) error) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := client.Do(req.Clone(ctx))
		delay := time.Duration(600*(1<<attempt))*time.Millisecond + time.Duration(rand.IntN(250))*time.Millisecond
		retry := false
		if err != nil {
			var ne net.Error
			retry = (errors.As(err, &ne) && ne.Timeout()) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET)
		} else if !strings.EqualFold(resp.Header.Get("Cf-Mitigated"), "challenge") {
			switch resp.StatusCode {
			case 408, 500, 502, 503, 504, 520, 521, 522, 523, 524, 525:
				retry = true
			case 429:
				// A rate limit without a usable server deadline requires user intervention.
				_, retry = retryAfter(resp.Header.Get("Retry-After"), time.Now())
			}
			if d, ok := retryAfter(resp.Header.Get("Retry-After"), time.Now()); ok && d > delay {
				delay = d
			}
		}
		if ctx.Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return nil, ctx.Err()
		}
		if !retry || attempt >= 3 {
			return resp, err
		}
		// Do not retry earlier than Retry-After, or hold a response until a deadline
		// when there is not enough time left for another useful attempt.
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay+time.Second {
			return resp, err
		}
		if resp != nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
		if err := wait(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func retryAfter(value string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Duration(0), when.Sub(now)), true
	}
	return 0, false
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
