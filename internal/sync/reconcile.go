package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"git-drive-sync/internal/gitignore"
	"git-drive-sync/internal/log"
)

// mtimeSkew is the tolerance applied to mtime comparisons. We Chtimes the
// destination to src's mtime after every copy, so in steady state the two
// match within filesystem resolution. The small skew absorbs sub-second
// rounding and OneDrive's habit of nudging dst.mtime forward after upload.
const mtimeSkew = 100 * time.Millisecond

type fileMeta struct {
	size  int64
	mtime time.Time
}

// Reconcile walks both source and destination, copying any source files
// missing or stale on the destination, and deleting any destination entries
// that no longer exist in source. This is the rewind-detection mechanism.
func Reconcile(ctx context.Context, m *gitignore.Matcher, s *Syncer) error {
	srcSet, err := walkSrc(s.SrcRoot, m)
	if err != nil {
		return fmt.Errorf("walk src: %w", err)
	}
	dstFiles, dstDirs, err := walkDst(s.DstRoot)
	if err != nil {
		return fmt.Errorf("walk dst: %w", err)
	}

	// Stage 1: copy missing or stale source files.
	var toCopy []string
	for rel, sm := range srcSet {
		dm, ok := dstFiles[rel]
		if !ok {
			toCopy = append(toCopy, rel)
			continue
		}
		if sm.size != dm.size || sm.mtime.After(dm.mtime.Add(mtimeSkew)) {
			toCopy = append(toCopy, rel)
		}
	}
	sort.Strings(toCopy)
	for _, rel := range toCopy {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.CopyFile(ctx, rel); err != nil {
			log.Warn("reconcile copy failed", "rel", rel, "err", err)
		}
	}

	// Stage 2: delete dst files with no counterpart in src (rewind detection).
	var toDelete []string
	for rel := range dstFiles {
		if _, ok := srcSet[rel]; !ok {
			toDelete = append(toDelete, rel)
		}
	}
	// Deepest first so we delete files before their containing dirs.
	sort.Sort(sort.Reverse(sort.StringSlice(toDelete)))
	for _, rel := range toDelete {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.DeleteFile(ctx, rel); err != nil {
			log.Warn("reconcile delete failed", "rel", rel, "err", err)
		}
	}

	// Stage 3: remove empty dst dirs that have no src counterpart.
	sort.Sort(sort.Reverse(sort.StringSlice(dstDirs)))
	for _, rel := range dstDirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		srcDir := filepath.Join(s.SrcRoot, rel)
		info, err := os.Stat(srcDir)
		if err != nil || !info.IsDir() {
			if err := s.DeleteDir(ctx, rel); err != nil {
				log.Debug("reconcile rmdir skipped", "rel", rel, "err", err)
			}
		}
	}
	return nil
}

func walkSrc(root string, m *gitignore.Matcher) (map[string]fileMeta, error) {
	out := make(map[string]fileMeta)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if m.IsIgnored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if m.IsIgnored(rel, false) {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			log.Debug("symlink skipped", "rel", rel)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		out[rel] = fileMeta{size: info.Size(), mtime: info.ModTime()}
		return nil
	})
	return out, err
}

func walkDst(root string) (map[string]fileMeta, []string, error) {
	files := make(map[string]fileMeta)
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if IsTempFile(filepath.Base(path)) {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, rel)
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil
		}
		// dst-side symlinks (manually placed by the user, say) should be
		// removed since they have no src counterpart — record them as files
		// so the diff picks them up for deletion.
		files[rel] = fileMeta{size: info.Size(), mtime: info.ModTime()}
		return nil
	})
	return files, dirs, err
}
