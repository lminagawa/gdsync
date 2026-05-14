//go:build windows

package sync

import (
	"strings"

	"golang.org/x/sys/windows"
)

// longPath prefixes paths over the legacy 260-char limit with \\?\ so the
// Win32 API uses the long-path variant.
func longPath(p string) string {
	if len(p) < 248 {
		return p
	}
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		// UNC path → \\?\UNC\server\share\...
		return `\\?\UNC\` + strings.TrimPrefix(p, `\\`)
	}
	return `\\?\` + p
}

// osRename uses MoveFileEx so that an existing destination is replaced
// atomically and the move is flushed to disk before returning.
func osRename(src, dst string) error {
	srcU, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstU, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(srcU, dstU,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
