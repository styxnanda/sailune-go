//go:build ignore

// Run explicitly: go run scripts/check-scraping.go [-timeout 15s] URL ...
// Guest-only, read-only probe: never opens the user's library or sessions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	sailune "github.com/styxnanda/sailune-go"
	"github.com/styxnanda/sailune-go/browser"
	"os"
	"time"
)

func main() {
	timeout := flag.Duration("timeout", 30*time.Second, "total budget per story")
	profile := flag.String("browser-profile", "", "optional app-owned background Chrome profile")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/check-scraping.go [-timeout 15s] URL ...")
		os.Exit(2)
	}
	success := 0
	scraper := &sailune.Scraper{}
	if *profile != "" {
		scraper.Browser = &sailune.BrowserRecovery{Loader: &browser.Chromium{Profile: *profile}}
	}
	for i, u := range flag.Args() {
		if i > 0 {
			time.Sleep(time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		start := time.Now()
		m, err := scraper.Fetch(ctx, u)
		cancel()
		row := struct {
			URL          string `json:"url"`
			Success      bool   `json:"success"`
			Milliseconds int64  `json:"milliseconds"`
			Error        string `json:"error,omitempty"`
		}{URL: u, Success: err == nil && m.Title != "", Milliseconds: time.Since(start).Milliseconds()}
		if err != nil {
			row.Error = err.Error()
		}
		if row.Success {
			success++
		}
		json.NewEncoder(os.Stdout).Encode(row)
	}
	fmt.Fprintf(os.Stderr, "Fetched %d/%d stories (%.1f%%)\n", success, flag.NArg(), 100*float64(success)/float64(flag.NArg()))
	if success != flag.NArg() {
		os.Exit(1)
	}
}
