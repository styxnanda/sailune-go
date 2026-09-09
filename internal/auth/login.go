package auth

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"time"

	"github.com/styxnanda/sailune-go/internal/model"
)

// LoginURL opens the official site, where the user controls the sign-in flow.
func LoginURL(site Site) (string, error) {
	host, err := model.SiteHost(site)
	if err != nil {
		return "", err
	}
	return "https://" + host + "/", nil
}

// OpenLoginBrowser opens the OS default browser. It does not read cookies or
// detect login completion. Frontends must obtain consent before importing.
func OpenLoginBrowser(ctx context.Context, site Site) error {
	name, args, err := loginBrowserCommand(runtime.GOOS, site)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return errors.New("could not open the default browser; open the site manually and use auth SITE --cookies-from-browser BROWSER")
	}
	return nil
}

func loginBrowserCommand(platform string, site Site) (string, []string, error) {
	u, err := LoginURL(site)
	if err != nil {
		return "", nil, err
	}
	switch platform {
	case "darwin":
		return "/usr/bin/open", []string{u}, nil
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", u}, nil
	case "linux":
		return "xdg-open", []string{u}, nil
	default:
		return "", nil, errors.New("opening the default browser is supported on macOS, Linux, and Windows")
	}
}
