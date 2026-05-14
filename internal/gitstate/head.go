package gitstate

import (
	"context"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"git-drive-sync/internal/log"
)

// Watch starts a goroutine that fires a token on changes to any of the Git
// state files that imply the working tree may have shifted (HEAD, branch
// refs, packed-refs, index). The caller is expected to trigger a reconcile
// in response.
func Watch(ctx context.Context, gitDir string, fire chan<- struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := w.Add(gitDir); err != nil {
		w.Close()
		return err
	}
	refsDir := filepath.Join(gitDir, "refs")
	if err := addRecursive(w, refsDir); err != nil {
		log.Debug("gitstate add refs failed", "err", err)
	}

	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if shouldFire(ev) {
					select {
					case fire <- struct{}{}:
					default:
					}
				}
				if ev.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
						_ = w.Add(ev.Name)
					}
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Debug("gitstate watcher error", "err", err)
			}
		}
	}()
	return nil
}

func shouldFire(ev fsnotify.Event) bool {
	base := filepath.Base(ev.Name)
	switch base {
	case "HEAD", "packed-refs", "index", "ORIG_HEAD", "MERGE_HEAD", "FETCH_HEAD":
		return true
	}
	// Anything under refs/heads or refs/tags counts as a ref move.
	parent := filepath.Base(filepath.Dir(ev.Name))
	if parent == "heads" || parent == "tags" || parent == "remotes" {
		return true
	}
	return false
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}
