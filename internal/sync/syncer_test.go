package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	syncpkg "git-drive-sync/internal/sync"
)

func TestCopyFileBasic(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	content := []byte("hello gdsync")
	writeFile(t, filepath.Join(src, "a.txt"), content)

	s := syncpkg.New(src, dst, syncpkg.DefaultPolicy(3), false)
	if err := s.CopyFile(context.Background(), "a.txt"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestCopyFileSymlinkSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privilege on Windows")
	}
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "target.txt"), []byte("x"))
	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	s := syncpkg.New(src, dst, syncpkg.DefaultPolicy(3), false)
	if err := s.CopyFile(context.Background(), "link.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected symlink not copied, got err=%v", err)
	}
}

func TestDeleteFileMissingIsOK(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	s := syncpkg.New(src, dst, syncpkg.DefaultPolicy(3), false)
	if err := s.DeleteFile(context.Background(), "nope.txt"); err != nil {
		t.Fatalf("delete of missing file should be no-op, got %v", err)
	}
}

func TestCopyFileCreatesNestedDirs(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	deep := filepath.Join(src, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(deep, "d.txt"), []byte("deep"))

	s := syncpkg.New(src, dst, syncpkg.DefaultPolicy(3), false)
	if err := s.CopyFile(context.Background(), filepath.Join("a", "b", "c", "d.txt")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "a", "b", "c", "d.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 4 {
		t.Fatalf("size = %d, want 4", info.Size())
	}
}

func TestCopyFileTempFileNotLeft(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "x.txt"), []byte("y"))

	s := syncpkg.New(src, dst, syncpkg.DefaultPolicy(3), false)
	if err := s.CopyFile(context.Background(), "x.txt"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if syncpkg.IsTempFile(e.Name()) {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate so mtime comparison tests are reliable on case-skewed FSes.
	_ = os.Chtimes(path, time.Now(), time.Now())
}
