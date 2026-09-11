//go:build !windows

package auth

import (
	"io/fs"
	"os"
	"syscall"
)

func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// checkPrivate requires owner-only mode bits and current-user ownership.
func checkPrivate(info fs.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errNotPrivate
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return errNotPrivate
	}
	return nil
}
