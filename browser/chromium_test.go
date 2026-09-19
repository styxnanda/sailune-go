//go:build !android

package browser

import (
	"context"
	"github.com/chromedp/chromedp"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt in because this test launches the installed Chrome browser headlessly.
func TestRealChromiumExtraction(t *testing.T) {
	if os.Getenv("SAILUNE_BROWSER_TEST") != "1" {
		t.Skip("set SAILUNE_BROWSER_TEST=1 for real browser integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	alloc, stop := chromedp.NewExecAllocator(ctx, chromedp.DefaultExecAllocatorOptions[:]...)
	defer stop()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	html := `<script>setTimeout(()=>{document.body.innerHTML='<div id="profile_top"><b class="xcontrast_txt">Fixture title</b><a href="/u/1/Author">Author</a></div><div id="storytext">SECRET CHAPTER</div>'},100)</script>`
	if err := chromedp.Run(tab, chromedp.Navigate("data:text/html,"+url.PathEscape(html))); err != nil {
		t.Fatal(err)
	}
	_, body, err := extract(tab)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Fixture title") || strings.Contains(body, "SECRET CHAPTER") {
		t.Fatal("incorrect header extraction")
	}
}
