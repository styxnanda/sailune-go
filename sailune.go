// Package sailune provides the public API for local fanfiction bookmarks,
// metadata fetching, and browser sessions. Implementations live in internal
// packages; aliases preserve existing imports, struct literals, and methods.
package sailune

import (
	"context"
	"io"

	"github.com/styxnanda/sailune-go/internal/auth"
	"github.com/styxnanda/sailune-go/internal/library"
	"github.com/styxnanda/sailune-go/internal/model"
	"github.com/styxnanda/sailune-go/internal/scrape"
)

type (
	Site            = model.Site
	Status          = model.Status
	Bookmark        = model.Bookmark
	Metadata        = model.Metadata
	Library         = library.Library
	Store           = library.Store
	Filter          = library.Filter
	Patch           = library.Patch
	MetadataFetcher = library.MetadataFetcher
	SessionStore    = auth.SessionStore
	SessionStatus   = auth.SessionStatus
	BrowserSpec     = auth.BrowserSpec
	Scraper         = scrape.Scraper
)

const (
	AO3       = model.AO3
	FFN       = model.FFN
	Planned   = model.Planned
	Reading   = model.Reading
	Completed = model.Completed
	OnHold    = model.OnHold
	Dropped   = model.Dropped
)

var (
	ErrNotFound        = library.ErrNotFound
	ErrDuplicate       = library.ErrDuplicate
	ErrLoginRequired   = scrape.ErrLoginRequired
	ErrChallenge       = scrape.ErrChallenge
	ErrWorkUnavailable = scrape.ErrWorkUnavailable
	ErrRateLimited     = scrape.ErrRateLimited
	ErrMetadata        = scrape.ErrMetadata
)

func NormalizeURL(raw string) (string, Site, string, error)  { return model.NormalizeURL(raw) }
func ParseBrowserSpec(value string) (BrowserSpec, error)     { return auth.ParseBrowserSpec(value) }
func ParseMetadata(site Site, r io.Reader) (Metadata, error) { return scrape.ParseMetadata(site, r) }
func LoginURL(site Site) (string, error)                     { return auth.LoginURL(site) }
func OpenLoginBrowser(ctx context.Context, site Site) error  { return auth.OpenLoginBrowser(ctx, site) }

// DefaultSessionDir returns the local session directory, independent of bookmarks.
func DefaultSessionDir() (string, error) { return auth.DefaultSessionDir() }
