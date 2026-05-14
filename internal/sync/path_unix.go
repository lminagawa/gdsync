//go:build !windows

package sync

import "os"

func longPath(p string) string { return p }

func osRename(src, dst string) error {
	return os.Rename(src, dst)
}
