//go:build windows

package sync

import (
	"errors"

	"golang.org/x/sys/windows"
)

// ERROR_CLOUD_FILE_IN_USE is not (yet) exported by x/sys/windows.
const errCloudFileInUse = 0x80070189

var retryableWindows = []windows.Errno{
	windows.ERROR_SHARING_VIOLATION,
	windows.ERROR_LOCK_VIOLATION,
	windows.ERROR_ACCESS_DENIED,
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	for _, code := range retryableWindows {
		if errors.Is(err, code) {
			return true
		}
	}
	var errno windows.Errno
	if errors.As(err, &errno) {
		if uint32(errno) == errCloudFileInUse {
			return true
		}
	}
	return false
}
