// Package platform contains desktop adapters, independent of library behavior.
package platform

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/styxnanda/sailune-go/internal/model"
)

func browserCommand(platform, raw string) (string, []string, error) {
	if _, _, _, err := model.NormalizeURL(raw); err != nil {
		return "", nil, err
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, errors.New("expected an HTTPS story URL without query or fragment")
	}
	switch platform {
	case "darwin":
		return "/usr/bin/open", []string{raw}, nil
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", raw}, nil
	case "linux":
		return "xdg-open", []string{raw}, nil
	default:
		return "", nil, errors.New("default browser opening supports macOS, Windows, and Linux")
	}
}

func OpenBrowser(ctx context.Context, raw string) error {
	name, args, err := browserCommand(runtime.GOOS, raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return errors.New("could not open the default browser; use --print-url to open the URL manually")
	}
	return nil
}
