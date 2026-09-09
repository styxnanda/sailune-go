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
		if _, err := fmt.Fprint(out, "After signing in, enter the browser you used (brave, chrome, chromium, edge, vivaldi, firefox).\nAdd :PROFILE if needed, e.g. brave:Default. Enter nothing to cancel.\nBrowser: "); err != nil {
			return sailune.SessionStatus{}, err
		}
		source, err = loginLine(ctx, input)
		if err != nil {
			return sailune.SessionStatus{}, err
		}
	}
	if source == "" || strings.EqualFold(source, "cancel") {
		return sailune.SessionStatus{}, errLoginCanceled
	}
	if _, err := sailune.ParseBrowserSpec(source); err != nil {
		return sailune.SessionStatus{}, err
	}
	if _, err := fmt.Fprintf(out, "Import and save only %s cookies from %s in %s?\nThis replaces any saved %s session. Cookies can grant account access and are stored locally without encryption.\nType yes after signing in to consent, or press Enter to cancel: ", site, source, store.Dir, site); err != nil {
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
	return store.ImportBrowser(ctx, site, source)
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
