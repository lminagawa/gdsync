//go:build darwin

package watcher

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsevents"
)

type darwinWatcher struct {
	stream *fsevents.EventStream
	events chan Event
	errors chan error
	cancel context.CancelFunc
}

func newPlatformWatcher(ctx context.Context, rootDir string, shouldWatch func(string) bool) (Watcher, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	dev, err := fsevents.DeviceForPath(abs)
	if err != nil {
		return nil, err
	}
	es := &fsevents.EventStream{
		Paths:   []string{abs},
		Latency: 100_000_000, // 100ms in nanoseconds
		Device:  dev,
		Flags:   fsevents.FileEvents | fsevents.WatchRoot,
	}
	es.Start()
	cctx, cancel := context.WithCancel(ctx)
	w := &darwinWatcher{
		stream: es,
		events: make(chan Event, 1024),
		errors: make(chan error, 1),
		cancel: cancel,
	}
	go w.loop(cctx, abs, shouldWatch)
	return w, nil
}

func (w *darwinWatcher) loop(ctx context.Context, root string, shouldWatch func(string) bool) {
	defer close(w.events)
	for {
		select {
		case <-ctx.Done():
			return
		case batch, ok := <-w.stream.Events:
			if !ok {
				return
			}
			for _, e := range batch {
				path := e.Path
				if !strings.HasPrefix(path, "/") {
					path = "/" + path
				}
				if shouldWatch != nil && !pathAllowed(root, path, shouldWatch) {
					continue
				}
				select {
				case w.events <- Event{Op: classifyFSE(e.Flags), Path: path}:
				default:
				}
			}
		}
	}
}

func pathAllowed(root, p string, shouldWatch func(string) bool) bool {
	// Climb from p toward root, asking shouldWatch about each ancestor dir
	// (inclusive of p's directory). If any ancestor is excluded, the file
	// itself is excluded.
	dir := filepath.Dir(p)
	for {
		if !shouldWatch(dir) {
			return false
		}
		if dir == root || dir == "/" || dir == filepath.Dir(dir) {
			return true
		}
		dir = filepath.Dir(dir)
	}
}

func classifyFSE(flags fsevents.EventFlags) Op {
	switch {
	case flags&fsevents.ItemRemoved != 0:
		return OpRemove
	case flags&fsevents.ItemRenamed != 0:
		return OpRename
	case flags&fsevents.ItemCreated != 0:
		return OpCreate
	case flags&fsevents.ItemModified != 0:
		return OpWrite
	case flags&fsevents.ItemChangeOwner != 0, flags&fsevents.ItemInodeMetaMod != 0:
		return OpChmod
	}
	return OpWrite
}

func (w *darwinWatcher) Events() <-chan Event { return w.events }
func (w *darwinWatcher) Errors() <-chan error { return w.errors }
func (w *darwinWatcher) Close() error {
	w.cancel()
	w.stream.Stop()
	return nil
}
