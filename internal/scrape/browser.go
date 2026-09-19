package scrape

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/styxnanda/sailune-go/internal/model"
)

// BrowserLoader loads a page without UI and returns its final URL and bounded
// metadata HTML. Implementations must stop loading when ctx is cancelled.
type BrowserLoader interface {
	Load(context.Context, string) (string, string, error)
}

// BrowserRecovery serializes browser work and cools down failed challenges.
// Reuse one instance across requests. Cookies stay in the browser's own profile.
type BrowserRecovery struct {
	Loader  BrowserLoader
	mu      sync.Mutex
	busy    bool
	retryAt time.Time
}

func (b *BrowserRecovery) fetch(ctx context.Context, canonical string) (Metadata, error) {
	b.mu.Lock()
	if b.busy || time.Now().Before(b.retryAt) {
		b.mu.Unlock()
		return Metadata{}, errors.New("website details unavailable; browser recovery is cooling down or busy")
	}
	b.busy = true
	b.mu.Unlock()
	failed := true
	defer func() {
		b.mu.Lock()
		b.busy = false
		if failed && !errors.Is(ctx.Err(), context.Canceled) {
			b.retryAt = time.Now().Add(time.Minute)
		}
		b.mu.Unlock()
	}()
	final, body, err := b.Loader.Load(ctx, canonical)
	if ctx.Err() != nil {
		return Metadata{}, ctx.Err()
	}
	if err != nil {
		return Metadata{}, errors.New("website details unavailable after background browser recovery")
	}
	u, e := url.Parse(final)
	normalized, site, _, nerr := model.NormalizeURL(final)
	if e != nil || u.Scheme != "https" || u.User != nil || nerr != nil || site != FFN || normalized != canonical {
		return Metadata{}, errors.New("browser returned a different story or origin")
	}
	if len(body) > 1<<20 {
		return Metadata{}, errors.New("browser metadata exceeds 1 MiB")
	}
	m, err := ParseMetadata(FFN, strings.NewReader(body))
	if err != nil {
		return Metadata{}, errors.New("website details unavailable; background browser remained blocked")
	}
	m.FetchedAt = time.Now().UTC()
	failed = false
	return m, nil
}

func (s *Scraper) Fetch(ctx context.Context, raw string) (Metadata, error) {
	canonical, site, _, err := model.NormalizeURL(raw)
	if err != nil {
		return Metadata{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if site != FFN || s.Browser == nil || s.Browser.Loader == nil {
		return s.fetchHTTP(ctx, raw)
	}
	// Reserve time for the browser instead of exhausting the total HTTP budget.
	direct, stop := context.WithTimeout(ctx, 5*time.Second)
	m, err := s.fetchHTTP(direct, raw)
	stop()
	if ctx.Err() != nil {
		return Metadata{}, ctx.Err()
	}
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, ErrChallenge) && !errors.Is(err, ErrMetadata) && !errors.Is(err, context.DeadlineExceeded) {
		return Metadata{}, err
	}
	return s.Browser.fetch(ctx, canonical)
}
