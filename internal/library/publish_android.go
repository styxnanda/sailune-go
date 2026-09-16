//go:build android

package library

import "golang.org/x/sys/unix"

// Android's app-data SELinux policy denies hard links. Both paths are in the
// same app-private directory. RENAME_NOREPLACE atomically publishes the complete
// database without overwriting a database created by another caller. Do not
// fall back to a check followed by Rename: that would introduce a data-loss race.
func publishDatabase(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
