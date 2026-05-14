# gdsync — `git-drive-sync`

> **One-way sync from a Git working tree to your cloud-mounted folder, built for the AI-coding era.**
>
> Git is the truth. The cloud is the mirror. When your agent rewinds, the cloud rewinds.

[![Go 1.22+](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go)](https://go.dev) [![macOS · Windows](https://img.shields.io/badge/platforms-macOS%20%7C%20Windows-lightgrey)]() [![status: MVP](https://img.shields.io/badge/status-MVP-blue)]()

`gdsync` is a single-binary CLI that **mirrors a Git working tree, in one direction, to a destination directory on your local filesystem** — typically the local mount of OneDrive, Google Drive, Dropbox, or iCloud Drive. It watches the tree in real time, respects `.gitignore`, survives transient file-lock errors that cloud sync clients love to throw, and — crucially — **detects rewinds**: when your AI agent runs `git reset --hard` or its own `/rewind` and the working tree shrinks, the cloud folder shrinks with it.

---

## Why it exists / 開発の背景

Modern AI coding agents (Claude Code, Cursor, Aider, and friends) edit working trees in bursts: dozens of files written, deleted, refactored, then sometimes wholesale undone. Two things break under that workload when your project lives inside a cloud-synced folder:

1. **Cloud sync clients are bi-directional and stateful.** They diff *their* last-seen snapshot against the filesystem — not against the Git tree. When an agent deletes a file and then a rewind brings it *partly* back, the cloud's view of "deleted" gets confused. Files leak, ghosts return, conflicted copies appear.
2. **They can't tell "deleted by user" from "vanished because the timeline moved."** A `git reset --hard HEAD~5` that removes 40 untracked-but-staged files looks, to OneDrive, like a 40-file deletion event. Half the time the client mirrors the delete; the other half it "rescues" the files back onto disk because it thinks something went wrong.

The result is a project directory that drifts away from what Git says it is, polluted with stale artifacts the next agent will then re-edit. The cure is to **stop treating the cloud folder as a workspace** and start treating it as a mirror — refreshed continuously from a source of truth that already knows how to express intent: the Git working tree.

`gdsync` is that mirror.

> 日本語: Claude Code 等の AI エージェントは `\rewind` や `git reset` を多用します。一方 OneDrive / Google Drive のクライアントは独自のスナップショット差分でしか動かないため、「Git で消したはずのファイル」がクラウドに残る、あるいは復活する事故が起きがちです。`gdsync` は Git ツリーを唯一の真実とし、ローカルマウントへ単方向ミラーリングすることでこの種の事故を構造的に防ぎます。

---

## What it does

- **Watches** your Git working tree with FSEvents (macOS) or fsnotify (Linux/Windows).
- **Filters** every event through the repo's `.gitignore` (with nested-gitignore semantics matching Git itself) plus a hardcoded exclusion set: `.git/`, `.claude_code/`, `.cursor/`, `.DS_Store`, `*.tmp`, `node_modules/`.
- **Copies** changed files to `--dest` using a temp-then-rename atomic write, so cloud clients never see half-written files.
- **Deletes** files from `--dest` the moment they disappear from the source.
- **Reconciles** the two trees on a 30-second cadence and on every Git state change (`.git/HEAD`, refs, packed-refs, index). This is the rewind detector — if a file is gone from `src` it gets removed from `dst`, no exceptions.
- **Retries** with exponential backoff when the cloud client has the destination momentarily locked.
- **Skips** symlinks entirely, by design (see below).

---

## Architecture & design decisions

### Git as the single source of truth

`gdsync` is intentionally unidirectional. Two-way sync invents a new class of problem: conflict resolution. Once you have a conflict resolver, you have an authoritative version, and once you have an authoritative version you've reinvented Git. So the model here is much simpler:

- **`src` (Git working tree) → `dst` (cloud mount).** Always. Without exception.
- Anything that exists in `dst` but not in `src` is — by definition — wrong, and will be removed on the next reconcile.
- Users edit files in `src`. They read or share files via `dst`. They never need to think about which side is canonical, because only one side ever is.

This eliminates the entire surface area of merge conflicts, "who wins" semantics, and cloud-client interpretation. The cost is real: changes made directly inside the destination folder are lost. That is the deal, and it is exactly the right deal for the use case.

### Resilient I/O: exponential backoff for cloud-induced locks

Cloud-sync clients aggressively open, hash, and re-upload files they consider "interesting." During those windows, `open()` and `rename()` against the destination can fail with:

- POSIX: `EAGAIN`, `EBUSY`, `ETXTBSY`, `EACCES`
- Windows: `ERROR_SHARING_VIOLATION` (32), `ERROR_LOCK_VIOLATION` (33), `ERROR_ACCESS_DENIED` (5), `ERROR_CLOUD_FILE_IN_USE` (0x80070189)

These are transient. `gdsync` wraps every destination-side I/O operation in a classifier-driven retry: identify the error as retryable, back off (100 ms → 200 ms → 400 ms → … up to 30 s, jittered, max 8 attempts), and try again. Non-retryable errors (`ENOENT`, `ENOSPC`, etc.) fail fast so they're visible.

Writes are atomic: a temp file is created **inside the destination directory** (never `os.TempDir()` — that would cross volumes), `fsync`'d, then renamed over the final path. On Windows the rename goes through `MoveFileEx(... | MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)` via `golang.org/x/sys/windows`, so the operation is committed to disk before the call returns and cloud clients never observe a partial file.

### Symlinks: skipped on purpose

Symlink handling on Windows requires `SeCreateSymbolicLinkPrivilege`, which most user processes don't have. Adding "best-effort" symlink replication means shipping a feature that silently degrades on the second-most-important target platform, plus opening the door to symlink-cycle issues on the source side. `gdsync` declines that whole class of bug: symlinks in `src` are skipped at walk time, and any symlink that somehow appears in `dst` is treated as "has no `src` counterpart" and removed on the next reconcile. The behavior is identical on macOS and Windows, which is the whole point.

### Mtime + size, not content hashing

The reconciler compares files by `(size, mtime)` rather than by content hash. After every copy, `gdsync` explicitly sets `dst.mtime = src.mtime` via `Chtimes`, so the two should match exactly in steady state — with a 100 ms tolerance to absorb filesystem-resolution drift. OneDrive's habit of nudging `dst.mtime` *forward* after upload is tolerated by the comparison direction (only `src.mtime > dst.mtime + skew` triggers a copy). Content hashing was rejected because it linearly scans every byte on every pass, and the rewind-detection logic doesn't need it: a missing entry is a missing entry whether it's a 1-byte file or a 1-GB file.

### Per-platform watching

- **macOS** uses `github.com/fsnotify/fsevents`. FSEvents is natively recursive and cheap with file descriptors — a 50 k-directory repo costs essentially nothing to watch.
- **Linux / Windows** uses `github.com/fsnotify/fsnotify` with manual recursive descent. On Create events, the new subtree is walked and registered immediately, with synthetic Create events emitted for any children that appear before the watcher attaches (closing the well-known fsnotify race).

Per-path debouncing (default 1 s) collapses the editor save dance — write-temp + rename + fsync, sometimes repeated — into a single sync action keyed on the final filename.

### Git-state surveillance

A second watcher tracks `.git/HEAD`, `.git/refs/heads/`, `.git/packed-refs`, and `.git/index`. Any change there triggers an immediate reconcile. This is what catches:

- branch switches (`git checkout`)
- hard resets on the current branch (`git reset --hard`, where HEAD doesn't move but a ref does)
- index-only operations (`git checkout -- file`)
- garbage-collected refs (now in `packed-refs`)

Without this, you'd be waiting up to `--interval` seconds for the periodic reconcile to notice. With it, the cloud catches up within milliseconds of the Git operation completing.

---

## Installation

### Go install (recommended)

```sh
go install git-drive-sync/cmd/gdsync@latest
```

This drops the `gdsync` binary into `$(go env GOBIN)` (defaulting to `$HOME/go/bin`).

### Build from source

```sh
git clone <this repo> && cd git-drive-sync
make build           # → ./gdsync
make install         # → $GOBIN/gdsync
make dist            # → bin/gdsync-{darwin-arm64,darwin-amd64,windows-amd64.exe}
```

### Pre-built binaries

Cross-compiled binaries for macOS (arm64 + amd64) and Windows (amd64) are produced by `make dist`. Drop one onto your `PATH` and you're done — no runtime, no daemon, no service to install.

**Requires** Go 1.22+ for source builds. Pre-built binaries have no runtime dependency.

---

## Usage

### The zero-config path

```sh
# 1. Stand in a Git repo.
cd ~/Code/my-project

# 2. Point gdsync at the cloud-mounted folder you want to mirror into.
gdsync --dest ~/OneDrive/my-project
```

That's the whole interface. `gdsync` reads your `.gitignore`, copies everything else, then watches forever. Hit `Ctrl-C` and it flushes a final reconcile before exiting.

### Common flags

| Flag | Default | What it does |
|---|---|---|
| `--dest` | *(required)* | Destination directory (e.g. a OneDrive mount). Will be created if missing. |
| `--interval` | `30s` | How often to run the full reconcile pass. |
| `--debounce` | `1s` | How long to wait for follow-up events on the same path before syncing. |
| `--dry-run` | `false` | Log every action without touching the destination. |
| `-v`, `--verbose` | `false` | DEBUG-level logging (per-file copy/delete events). |
| `--once` | `false` | Run one full reconcile and exit. Good for cron / pre-commit hooks. |
| `--max-retries` | `8` | How hard to fight cloud-side file locks before giving up on a single file. |

### Recipes

**One-shot mirror, no watcher** — perfect for `cron`:

```sh
gdsync --dest ~/OneDrive/my-project --once
```

**Watch in dry-run while you tune `.gitignore`**:

```sh
gdsync --dest /tmp/inspect --dry-run -v
```

**Run as a background mirror on macOS** (until a `gdsync install` subcommand lands):

```sh
nohup gdsync --dest ~/OneDrive/my-project > ~/Library/Logs/gdsync.log 2>&1 &
```

---

## What's NOT in scope (yet)

By design, the MVP keeps the surface area small. The following are deliberately out of scope:

- **Bi-directional sync** — see "Git as the single source of truth" above. This will never be added.
- **Multiple destinations in one process** — run one `gdsync` per `--dest` for now.
- **`gdsync install` / launchd / systemd integration** — use `nohup` or your platform's service manager.
- **Configuration files** (`.gdsync.yaml`) — flags only, for now.
- **Conflict resolution UI / web dashboard** — there are no conflicts.

---

## Project layout

```
gdsync/
├── cmd/gdsync/main.go          # cobra entrypoint, event/dispatch loop
└── internal/
    ├── config/                 # flag parsing, path canonicalization
    ├── gitignore/              # matcher (uses go-git's gitignore parser)
    ├── watcher/                # FSEvents (darwin) + fsnotify (others)
    ├── sync/                   # backoff, retry classification, atomic copy, reconcile
    ├── gitstate/               # HEAD / refs / index watcher
    └── log/                    # slog wrapper with verbose toggle
```

The full architecture write-up lives in the design plan; the high-level flow is:

```
   fsnotify/FSEvents ─┐
                     ├─▶  debounce  ──▶  serialized sync worker  ──▶  dst
   git state watch  ─┤                                  ▲
                     │                                  │
   periodic ticker  ─┴──────────▶  reconcile  ──────────┘
```

A single mutex serializes all destination writes, so the reconciler and the live event handler can never race.

---

## Testing

```sh
go test ./...        # unit tests: gitignore matcher, backoff retry, syncer
make vet             # go vet ./...
```

Smoke-test the binary end-to-end:

```sh
# Initial sync, .gitignore enforcement, hardcoded exclusions, rewind detection.
SRC=$(mktemp -d) && DST=$(mktemp -d) && cd $SRC && git init -q
echo "*.log" > .gitignore
echo hello > a.txt && echo ignored > debug.log
gdsync --dest $DST --once -v
ls $DST                          # → a.txt, .gitignore
rm a.txt
gdsync --dest $DST --once -v
ls $DST                          # → just .gitignore (rewind propagated)
```

---

## License

MIT (LICENSE pending).

---

## Acknowledgements

Built on the shoulders of:

- [`spf13/cobra`](https://github.com/spf13/cobra) — CLI framework
- [`fsnotify/fsnotify`](https://github.com/fsnotify/fsnotify) and [`fsnotify/fsevents`](https://github.com/fsnotify/fsevents) — filesystem event delivery
- [`go-git/go-git`](https://github.com/go-git/go-git) — the only gitignore matcher in the Go ecosystem with faithful nested-`.gitignore` and negation semantics
- [`golang.org/x/sys/windows`](https://pkg.go.dev/golang.org/x/sys/windows) — `MoveFileEx` and the Win32 error catalogue
