package sync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"git-drive-sync/internal/log"
)

const tempSuffix = ".gdsync-tmp-"

type Syncer struct {
	SrcRoot string
	DstRoot string
	Backoff BackoffPolicy
	DryRun  bool
}

func New(srcRoot, dstRoot string, b BackoffPolicy, dryRun bool) *Syncer {
	return &Syncer{
		SrcRoot: srcRoot,
		DstRoot: dstRoot,
		Backoff: b,
		DryRun:  dryRun,
	}
}

func (s *Syncer) srcPath(rel string) string { return filepath.Join(s.SrcRoot, rel) }
func (s *Syncer) dstPath(rel string) string { return longPath(filepath.Join(s.DstRoot, rel)) }

// CopyFile copies a regular file from src to dst, atomically and with backoff
// retry on transient lock errors. Symlinks and other non-regular entries are
// silently skipped.
func (s *Syncer) CopyFile(ctx context.Context, rel string) error {
	src := s.srcPath(rel)
	info, err := os.Lstat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		log.Debug("symlink skipped", "rel", rel)
		return nil
	}
	if !info.Mode().IsRegular() {
		log.Debug("non-regular skipped", "rel", rel, "mode", info.Mode())
		return nil
	}

	if s.DryRun {
		log.Info("[dry-run] copy", "rel", rel)
		return nil
	}

	dst := s.dstPath(rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	if err := s.Backoff.Do(ctx, func() error {
		return atomicCopyFile(src, dst, info.Mode().Perm(), info.ModTime())
	}); err != nil {
		return err
	}
	log.Debug("copied", "rel", rel)
	return nil
}

func atomicCopyFile(src, dst string, srcPerm os.FileMode, srcMtime time.Time) error {
	dstDir := filepath.Dir(dst)
	tmpName, err := tempName(dstDir, filepath.Base(dst))
	if err != nil {
		return err
	}
	out, err := os.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		out.Close()
		os.Remove(tmpName)
		return err
	}
	_, copyErr := io.Copy(out, in)
	in.Close()
	if copyErr != nil {
		out.Close()
		os.Remove(tmpName)
		return copyErr
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmpName)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if runtime.GOOS != "windows" {
		mode := os.FileMode(0o644)
		if srcPerm&0o111 != 0 {
			mode = 0o755
		}
		_ = os.Chmod(tmpName, mode)
	}
	if err := osRename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	// Align dst mtime with src so reconcile can rely on equality. OneDrive
	// may later rewrite dst.mtime forward; that's tolerated by the skew in
	// reconcile.go.
	_ = os.Chtimes(dst, srcMtime, srcMtime)
	return nil
}

func tempName(dir, base string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return filepath.Join(dir, "."+base+tempSuffix+hex.EncodeToString(b[:])), nil
}

func (s *Syncer) DeleteFile(ctx context.Context, rel string) error {
	if s.DryRun {
		log.Info("[dry-run] delete", "rel", rel)
		return nil
	}
	dst := s.dstPath(rel)
	if err := s.Backoff.Do(ctx, func() error {
		err := os.Remove(dst)
		if err != nil && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}); err != nil {
		return err
	}
	log.Debug("deleted", "rel", rel)
	return nil
}

func (s *Syncer) DeleteDir(ctx context.Context, rel string) error {
	if s.DryRun {
		log.Info("[dry-run] rmdir", "rel", rel)
		return nil
	}
	dst := s.dstPath(rel)
	err := os.Remove(dst)
	if err == nil {
		log.Debug("rmdir", "rel", rel)
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// "directory not empty" is benign — children may still be in flight; let
	// the next reconcile pick it up.
	if strings.Contains(err.Error(), "not empty") {
		return nil
	}
	return err
}

// IsTempFile reports whether the given file name is a transient temp file
// produced by atomicCopyFile.
func IsTempFile(name string) bool {
	return strings.Contains(name, tempSuffix)
}
