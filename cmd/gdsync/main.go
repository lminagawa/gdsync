package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	stdsync "sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"git-drive-sync/internal/config"
	"git-drive-sync/internal/gitignore"
	"git-drive-sync/internal/gitstate"
	"git-drive-sync/internal/log"
	syncpkg "git-drive-sync/internal/sync"
	"git-drive-sync/internal/watcher"
)

var version = "0.1.0-dev"

func main() {
	cfg := &config.Config{}

	root := &cobra.Command{
		Use:           "gdsync",
		Short:         "Mirror a Git working tree to a destination directory (one-way)",
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cfg)
		},
	}
	root.Flags().StringVar(&cfg.DstRoot, "dest", "", "destination directory (required)")
	root.Flags().DurationVar(&cfg.Interval, "interval", 30*time.Second, "full reconcile interval")
	root.Flags().DurationVar(&cfg.Debounce, "debounce", time.Second, "fsnotify event debounce window")
	root.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "log only, don't sync")
	root.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "verbose logging")
	root.Flags().BoolVar(&cfg.Once, "once", false, "run one full reconcile then exit")
	root.Flags().IntVar(&cfg.MaxRetries, "max-retries", 8, "max retries on file-lock errors")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	})

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	log.SetVerbose(cfg.Verbose)

	matcher, err := gitignore.NewMatcher(cfg.SrcRoot)
	if err != nil {
		return fmt.Errorf("load gitignore: %w", err)
	}

	syncer := syncpkg.New(cfg.SrcRoot, cfg.DstRoot, syncpkg.DefaultPolicy(cfg.MaxRetries), cfg.DryRun)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// syncMu serializes all destination-side I/O so that fsnotify events and
	// reconcile passes don't race against each other.
	var syncMu stdsync.Mutex
	reconcileNow := func(label string) {
		syncMu.Lock()
		defer syncMu.Unlock()
		start := time.Now()
		log.Info("reconcile開始", "trigger", label)
		if err := syncpkg.Reconcile(ctx, matcher, syncer); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Error("reconcile失敗", "err", err)
			return
		}
		log.Info("reconcile完了", "elapsed", time.Since(start))
	}

	reconcileNow("startup")
	if cfg.Once {
		return nil
	}

	shouldWatch := func(absDir string) bool {
		rel, err := filepath.Rel(cfg.SrcRoot, absDir)
		if err != nil || strings.HasPrefix(rel, "..") {
			return false
		}
		if rel == "." || rel == "" {
			return true
		}
		return !matcher.IsIgnored(rel, true)
	}
	w, err := watcher.New(ctx, cfg.SrcRoot, shouldWatch)
	if err != nil {
		return fmt.Errorf("watcher: %w", err)
	}
	defer w.Close()

	reconcileCh := make(chan struct{}, 1)
	if err := gitstate.Watch(ctx, filepath.Join(cfg.SrcRoot, ".git"), reconcileCh); err != nil {
		log.Warn("gitstate watcher disabled", "err", err)
	}

	// Per-path debounce: track latest event timestamp; flush only when no
	// follow-up event has arrived inside the debounce window.
	pending := make(map[string]time.Time)
	var pendingMu stdsync.Mutex
	enqueue := func(path string) {
		pendingMu.Lock()
		pending[path] = time.Now()
		pendingMu.Unlock()
	}
	flush := func() {
		now := time.Now()
		pendingMu.Lock()
		var ready []string
		for p, t := range pending {
			if now.Sub(t) >= cfg.Debounce {
				ready = append(ready, p)
				delete(pending, p)
			}
		}
		pendingMu.Unlock()
		if len(ready) == 0 {
			return
		}

		syncMu.Lock()
		defer syncMu.Unlock()
		for _, abs := range ready {
			if err := ctx.Err(); err != nil {
				return
			}
			handleEvent(ctx, cfg.SrcRoot, abs, matcher, syncer)
		}
	}

	flushTicker := time.NewTicker(maxDuration(cfg.Debounce/2, 100*time.Millisecond))
	defer flushTicker.Stop()
	reconcileTicker := time.NewTicker(cfg.Interval)
	defer reconcileTicker.Stop()

	log.Info("gdsync 起動",
		"src", cfg.SrcRoot,
		"dst", cfg.DstRoot,
		"interval", cfg.Interval,
		"debounce", cfg.Debounce,
		"dry_run", cfg.DryRun)

	for {
		select {
		case <-sigCh:
			log.Info("終了シグナル受信。reconcile を一回流して終了します。")
			cancel()
			finalCtx, finalCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = syncpkg.Reconcile(finalCtx, matcher, syncer)
			finalCancel()
			return nil
		case ev, ok := <-w.Events():
			if !ok {
				return nil
			}
			if syncpkg.IsTempFile(filepath.Base(ev.Path)) {
				continue
			}
			enqueue(ev.Path)
		case err, ok := <-w.Errors():
			if !ok {
				continue
			}
			log.Warn("watcher error", "err", err)
		case <-flushTicker.C:
			flush()
		case <-reconcileTicker.C:
			reconcileNow("interval")
		case <-reconcileCh:
			reconcileNow("git-state")
		}
	}
}

func handleEvent(ctx context.Context, srcRoot, absPath string, m *gitignore.Matcher, s *syncpkg.Syncer) {
	rel, err := filepath.Rel(srcRoot, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	if rel == "." || rel == "" {
		return
	}

	info, statErr := os.Lstat(absPath)
	if statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			log.Debug("lstat error", "rel", rel, "err", statErr)
			return
		}
		// File no longer exists in src — propagate the delete unless the path
		// was ignored anyway.
		if m.IsIgnored(rel, false) {
			return
		}
		if err := s.DeleteFile(ctx, rel); err != nil {
			log.Warn("delete failed", "rel", rel, "err", err)
		}
		return
	}

	if info.IsDir() {
		// Directory events are handled by the periodic reconcile and by the
		// watcher's recursive add — nothing to do here.
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		log.Debug("symlink skipped", "rel", rel)
		return
	}
	if !info.Mode().IsRegular() {
		return
	}
	if m.IsIgnored(rel, false) {
		return
	}
	if err := s.CopyFile(ctx, rel); err != nil {
		log.Warn("copy failed", "rel", rel, "err", err)
	}
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
