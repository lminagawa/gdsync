//go:build !darwin

package watcher

import (
	"context"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"git-drive-sync/internal/log"
)

type fsnotifyWatcher struct {
	w      *fsnotify.Watcher
	events chan Event
	errors chan error
	cancel context.CancelFunc
	should func(string) bool
}

func newPlatformWatcher(ctx context.Context, rootDir string, shouldWatch func(string) bool) (Watcher, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithCancel(ctx)
	w := &fsnotifyWatcher{
		w:      fw,
		events: make(chan Event, 1024),
		errors: make(chan error, 1),
		cancel: cancel,
		should: shouldWatch,
	}
	if err := w.addRecursive(abs); err != nil {
		fw.Close()
		cancel()
		return nil, err
	}
	go w.loop(cctx)
	return w, nil
}

func (w *fsnotifyWatcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if w.should != nil && !w.should(path) {
			return filepath.SkipDir
		}
		if err := w.w.Add(path); err != nil {
			log.Debug("watcher add failed", "path", path, "err", err)
		}
		return nil
	})
}

func (w *fsnotifyWatcher) loop(ctx context.Context) {
	defer close(w.events)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			op := classify(ev.Op)
			w.send(Event{Op: op, Path: ev.Name})

			// New directories — register a watcher then emit synthetic
			// Create events for any children that exist now, so we don't
			// lose them in the gap between mkdir and Add().
			if ev.Op&fsnotify.Create != 0 {
				info, err := os.Stat(ev.Name)
				if err == nil && info.IsDir() {
					if w.should == nil || w.should(ev.Name) {
						if err := w.w.Add(ev.Name); err != nil {
							log.Debug("watcher add new dir failed", "path", ev.Name, "err", err)
						}
						_ = filepath.WalkDir(ev.Name, func(p string, d os.DirEntry, err error) error {
							if err != nil || p == ev.Name {
								return nil
							}
							if d.IsDir() {
								if w.should != nil && !w.should(p) {
									return filepath.SkipDir
								}
								_ = w.w.Add(p)
							}
							w.send(Event{Op: OpCreate, Path: p})
							return nil
						})
					}
				}
			}
		case err, ok := <-w.w.Errors:
			if !ok {
				return
			}
			select {
			case w.errors <- err:
			default:
			}
		}
	}
}

func (w *fsnotifyWatcher) send(ev Event) {
	select {
	case w.events <- ev:
	default:
		log.Warn("watcher event buffer full, dropping", "path", ev.Path)
	}
}

func classify(op fsnotify.Op) Op {
	switch {
	case op&fsnotify.Create != 0:
		return OpCreate
	case op&fsnotify.Remove != 0:
		return OpRemove
	case op&fsnotify.Rename != 0:
		return OpRename
	case op&fsnotify.Write != 0:
		return OpWrite
	case op&fsnotify.Chmod != 0:
		return OpChmod
	}
	return OpWrite
}

func (w *fsnotifyWatcher) Events() <-chan Event { return w.events }
func (w *fsnotifyWatcher) Errors() <-chan error { return w.errors }
func (w *fsnotifyWatcher) Close() error {
	w.cancel()
	return w.w.Close()
}
