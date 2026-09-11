//go:build windows

package auth

import (
	"io/fs"
	"os"
)

func openNoFollow(path string) (*os.File, error) {
	// Reparse points are rejected by the Lstat check in Load before opening.
	return os.OpenFile(path, os.O_RDONLY, 0)
}

// checkPrivate is a placeholder on Windows. %APPDATA% inherits a
// user-private DACL by default; explicit DACL verification is tracked in TODO.
func checkPrivate(info fs.FileInfo) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		return errNotPrivate
	}
	return nil
}
