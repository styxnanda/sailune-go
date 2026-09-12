//go:build !windows

package library

import "os"

func privateFile(path string) error { return os.Chmod(path, 0600) }

func privateDirectory(path string) error { return os.Chmod(path, 0700) }
