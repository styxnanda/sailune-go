package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func defaultSessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library/Application Support/Sailune/sessions"), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is required for local session storage")
		}
		return filepath.Join(base, "Sailune/sessions"), nil
	default:
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" || !filepath.IsAbs(base) {
			base = filepath.Join(home, ".local/state")
		}
		return filepath.Join(base, "sailune/sessions"), nil
	}
}
