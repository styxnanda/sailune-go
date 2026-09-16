//go:build !android

package library

import "os"

func publishDatabase(source, destination string) error {
	return os.Link(source, destination)
}
