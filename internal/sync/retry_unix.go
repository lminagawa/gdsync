//go:build !windows

package sync

import (
	"errors"
	"syscall"
)

var retryableUnix = []syscall.Errno{
	syscall.EAGAIN,
	syscall.EBUSY,
	syscall.ETXTBSY,
	syscall.EACCES,
}

var nonRetryableUnix = []syscall.Errno{
	syscall.ENOENT,
	syscall.ENOTDIR,
	syscall.EISDIR,
	syscall.ENOSPC,
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	for _, code := range nonRetryableUnix {
		if errors.Is(err, code) {
			return false
		}
	}
	for _, code := range retryableUnix {
		if errors.Is(err, code) {
			return true
		}
	}
	return false
}
