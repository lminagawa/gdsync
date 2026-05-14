package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	SrcRoot    string
	DstRoot    string
	Interval   time.Duration
	Debounce   time.Duration
	DryRun     bool
	Verbose    bool
	Once       bool
	MaxRetries int
}

func (c *Config) Validate() error {
	if c.DstRoot == "" {
		return errors.New("--dest is required")
	}
	dst, err := resolvePath(c.DstRoot)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}

	if c.SrcRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		c.SrcRoot = cwd
	}
	src, err := resolvePath(c.SrcRoot)
	if err != nil {
		return fmt.Errorf("resolve src: %w", err)
	}
	c.SrcRoot = src
	c.DstRoot = dst

	gitDir := filepath.Join(c.SrcRoot, ".git")
	info, err := os.Stat(gitDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("not a Git repository: %s (.git directory not found)", c.SrcRoot)
	}

	if err := os.MkdirAll(c.DstRoot, 0o755); err != nil {
		return fmt.Errorf("ensure dest exists: %w", err)
	}
	// Now that dst exists on disk, re-resolve to canonicalize any symlinks
	// in its path so the watcher's event paths can be compared against it.
	if resolved, err := filepath.EvalSymlinks(c.DstRoot); err == nil {
		c.DstRoot = resolved
	}
	if c.DstRoot == c.SrcRoot {
		return errors.New("--dest must differ from source directory")
	}
	if isSubPath(c.SrcRoot, c.DstRoot) {
		return errors.New("--dest must not be inside the source Git repository")
	}

	if c.Interval <= 0 {
		c.Interval = 30 * time.Second
	}
	if c.Debounce <= 0 {
		c.Debounce = time.Second
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 8
	}
	return nil
}

// resolvePath returns an absolute, symlink-canonicalized path. macOS in
// particular routes /tmp and /var through /private symlinks, and FSEvents
// emits canonical paths — without this step the watcher path filter rejects
// every event because filepath.Rel returns "../private/...".
func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Path may not exist yet (e.g. --dest). Fall back to abs; the caller
		// is responsible for creating the path before we re-resolve.
		return abs, nil
	}
	return resolved, nil
}

func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	// rel does not start with ".." → child is inside parent
	if len(rel) >= 2 && rel[0] == '.' && rel[1] == '.' {
		return false
	}
	return true
}
