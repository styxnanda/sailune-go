//go:build !android

// Package browser supplies the optional silent desktop/CLI fetch adapter.
package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	sailune "github.com/styxnanda/sailune-go"
)

// Chromium uses an application-owned profile, never the user's browser profile.
// Processes are started on demand and shut down after each recovery attempt.
type Chromium struct{ Profile string }

func (c *Chromium) Load(ctx context.Context, u string) (string, string, error) {
	canonical, site, _, err := sailune.NormalizeURL(u)
	if err != nil || site != sailune.FFN {
		return "", "", errors.New("unsupported browser target")
	}
	if c.Profile == "" || !filepath.IsAbs(c.Profile) {
		return "", "", errors.New("absolute browser profile required")
	}
	if err := os.MkdirAll(c.Profile, 0700); err != nil {
		return "", "", err
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.UserDataDir(c.Profile), chromedp.Flag("disable-gpu", true), chromedp.Flag("mute-audio", true))
	alloc, stop := chromedp.NewExecAllocator(ctx, opts...)
	defer stop()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	if err := chromedp.Run(tab, network.Enable(), network.SetBlockedURLs([]string{"*.mp4", "*.webm", "*.mp3", "*.woff", "*.woff2"}), chromedp.Navigate(canonical)); err != nil {
		return "", "", err
	}
	return extract(tab)
}

func extract(tab context.Context) (string, string, error) {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		var page struct {
			URL  string `json:"url"`
			HTML string `json:"html"`
		}
		err := chromedp.Run(tab, chromedp.Evaluate(`(()=>{const p=document.querySelector('#profile_top');if(!p||!p.querySelector('a[href^="/u/"]'))return {url:location.href,html:''}; const h=p.outerHTML+(document.querySelector('#pre_story_links')?.outerHTML||'');return {url:location.href,html:h.length<=524288?h:''}})()`, &page))
		if err != nil {
			return "", "", err
		}
		if page.HTML != "" {
			return page.URL, page.HTML, nil
		}
		select {
		case <-tab.Done():
			return "", "", tab.Err()
		case <-ticker.C:
		}
	}
}
