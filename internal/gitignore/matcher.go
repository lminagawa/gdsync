package gitignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	gi "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// hardcodedDefaults are always ignored regardless of .gitignore contents.
// These exist to keep AI-tooling state and OS debris out of the synced tree.
var hardcodedDefaults = []string{
	".git/",
	".claude_code/",
	".cursor/",
	".DS_Store",
	"*.tmp",
	"node_modules/",
}

type Matcher struct {
	rootDir string
	m       gi.Matcher
}

func NewMatcher(rootDir string) (*Matcher, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	patterns, err := loadAllGitignore(abs)
	if err != nil {
		return nil, err
	}
	for _, p := range hardcodedDefaults {
		patterns = append(patterns, gi.ParsePattern(p, nil))
	}
	return &Matcher{
		rootDir: abs,
		m:       gi.NewMatcher(patterns),
	}, nil
}

func loadAllGitignore(root string) ([]gi.Pattern, error) {
	var patterns []gi.Pattern
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// Don't descend into directories that are always ignored — avoids
			// reading random .gitignore files inside node_modules etc.
			if path != root {
				switch name {
				case ".git", "node_modules", ".claude_code", ".cursor":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if d.Name() != ".gitignore" {
			return nil
		}
		relDir, _ := filepath.Rel(root, filepath.Dir(path))
		domain := splitDomain(relDir)
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r\n")
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			patterns = append(patterns, gi.ParsePattern(line, domain))
		}
		return nil
	})
	return patterns, err
}

func splitDomain(rel string) []string {
	if rel == "" || rel == "." {
		return nil
	}
	return strings.Split(filepath.ToSlash(rel), "/")
}

// IsIgnored returns true if relPath (relative to the matcher root, OS separator)
// should be excluded from sync.
func (m *Matcher) IsIgnored(relPath string, isDir bool) bool {
	if relPath == "" || relPath == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	return m.m.Match(parts, isDir)
}

func (m *Matcher) RootDir() string { return m.rootDir }
