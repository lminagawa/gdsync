package watcher

import "context"

type Op int

const (
	OpCreate Op = iota
	OpWrite
	OpRemove
	OpRename
	OpChmod
)

func (o Op) String() string {
	switch o {
	case OpCreate:
		return "create"
	case OpWrite:
		return "write"
	case OpRemove:
		return "remove"
	case OpRename:
		return "rename"
	case OpChmod:
		return "chmod"
	}
	return "unknown"
}

type Event struct {
	Op   Op
	Path string // absolute path
}

type Watcher interface {
	Events() <-chan Event
	Errors() <-chan error
	Close() error
}

// New creates a platform-appropriate recursive watcher rooted at rootDir.
// shouldWatch, when non-nil, is consulted before adding a directory; returning
// false causes the directory (and its subtree) to be skipped.
func New(ctx context.Context, rootDir string, shouldWatch func(absDir string) bool) (Watcher, error) {
	return newPlatformWatcher(ctx, rootDir, shouldWatch)
}
