package gitignore_test

import (
	"os"
	"path/filepath"
	"testing"

	"git-drive-sync/internal/gitignore"
)

func TestHardcodedDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "")

	m, err := gitignore.NewMatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{".git/HEAD", false, true},
		{".DS_Store", false, true},
		{"file.tmp", false, true},
		{"node_modules/foo.js", false, true},
		{".claude_code/log.txt", false, true},
		{".cursor/x", false, true},
		{"normal.txt", false, false},
		{"src/main.go", false, false},
	}
	for _, c := range cases {
		got := m.IsIgnored(c.rel, c.isDir)
		if got != c.want {
			t.Errorf("IsIgnored(%q, %v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
}

func TestRootGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "*.log\nbuild/\n")

	m, err := gitignore.NewMatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"build/output", false, true},
		{"build", true, true},
		{"src/main.go", false, false},
	}
	for _, c := range cases {
		got := m.IsIgnored(c.rel, c.isDir)
		if got != c.want {
			t.Errorf("IsIgnored(%q, %v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
}

func TestNestedGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	mustMkdir(t, filepath.Join(dir, "sub"))
	// A nested .gitignore that adds a local rule.
	writeFile(t, filepath.Join(dir, "sub", ".gitignore"), "secret.txt\n")

	m, err := gitignore.NewMatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rel  string
		want bool
	}{
		{"sub/secret.txt", true},
		{"sub/public.txt", false},
		{"public.txt", false},
		{"secret.txt", false}, // nested rule must not leak to root
		{"sub/app.log", true}, // root rule still applies
	}
	for _, c := range cases {
		got := m.IsIgnored(c.rel, false)
		if got != c.want {
			t.Errorf("IsIgnored(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
