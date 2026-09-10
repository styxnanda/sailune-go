package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	sailune "github.com/styxnanda/sailune-go"
)

type loginDependencies struct {
	input io.Reader
	open  func(context.Context, sailune.Site) error
}

var errLoginCanceled = errors.New("login canceled; no browser cookies were imported")

func interactiveLogin(ctx context.Context, site sailune.Site, source string, store sailune.SessionStore, out io.Writer, deps loginDependencies) (sailune.SessionStatus, error) {
	u, err := sailune.LoginURL(site)
	if err != nil {
		return sailune.SessionStatus{}, err
	}
	if ctx.Err() != nil {
		return sailune.SessionStatus{}, ctx.Err()
	}
	if _, err := fmt.Fprintf(out, "Opening %s in your default browser. Sign in there, then return here.\nSailune will wait for your consent before reading cookies.\n", u); err != nil {
		return sailune.SessionStatus{}, err
	}
	if err := deps.open(ctx, site); err != nil {
		return sailune.SessionStatus{}, err
	}
	input := bufio.NewScanner(deps.input)
	if source == "" {
		if _, err := fmt.Fprint(out, "After signing in, select a browser family: chromium or gecko (Firefox).\nYou can also enter a full source such as chromium/brave:Default or gecko:/absolute/profile/path.\nEnter nothing to cancel.\nBrowser family: "); err != nil {
			return sailune.SessionStatus{}, err
		}
		source, err = loginLine(ctx, input)
		if err != nil {
			return sailune.SessionStatus{}, err
		}
		family := strings.ToLower(source)
		if family == "chromium" || family == "gecko" {
			choices := "brave, chrome, chromium, edge, opera, vivaldi"
			if family == "gecko" {
				choices = "firefox; for a compatible fork use firefox:/absolute/profile/path"
			}
			if _, err := fmt.Fprintf(out, "Select the %s browser and optional :PROFILE (%s).\nBrowser: ", family, choices); err != nil {
				return sailune.SessionStatus{}, err
			}
			browser, err := loginLine(ctx, input)
			if err != nil {
				return sailune.SessionStatus{}, err
			}
			if browser == "" || strings.EqualFold(browser, "cancel") {
				return sailune.SessionStatus{}, errLoginCanceled
			}
			source = family + "/" + browser
		}
	}
	if source == "" || strings.EqualFold(source, "cancel") {
		return sailune.SessionStatus{}, errLoginCanceled
	}
	if _, err := sailune.ParseBrowserSpec(source); err != nil {
		return sailune.SessionStatus{}, err
	}
	if _, err := fmt.Fprintf(out, "Import and save only %s cookies from %s in %s?\nThis replaces any saved %s session. Cookies can grant account access and are encrypted locally with a key held in the OS credential store.\nType yes after signing in to consent, or press Enter to cancel: ", site, source, store.Dir, site); err != nil {
		return sailune.SessionStatus{}, err
	}
	answer, err := loginLine(ctx, input)
	if err != nil {
		return sailune.SessionStatus{}, err
	}
	if !strings.EqualFold(answer, "yes") {
		return sailune.SessionStatus{}, errLoginCanceled
	}
	if ctx.Err() != nil {
		return sailune.SessionStatus{}, ctx.Err()
	}
	status, err := store.ImportBrowser(ctx, site, source)
	if err == nil {
		fmt.Fprintln(out, "Cookies saved locally. Website login has not been verified; add a work to test access.")
	}
	return status, err
}

// Cancellation stops the flow even while stdin is waiting. The read goroutine
// cannot import cookies; only the caller can do so after affirmative consent.
func loginLine(ctx context.Context, input *bufio.Scanner) (string, error) {
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if !input.Scan() {
			ch <- result{err: errLoginCanceled}
			return
		}
		ch <- result{text: strings.TrimSpace(input.Text())}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.text, r.err
	}
}
