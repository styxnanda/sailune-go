package library

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func DefaultLibraryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var base string
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(home, "Library/Application Support/Sailune")
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is required for local library storage")
		}
		base = filepath.Join(base, "Sailune")
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if base == "" || !filepath.IsAbs(base) {
			base = filepath.Join(home, ".local/state")
		}
		base = filepath.Join(base, "sailune")
	}
	return filepath.Join(base, "library.sqlite3"), nil
}
