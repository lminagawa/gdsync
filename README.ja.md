# gdsync — `git-drive-sync`

> Git のワーキングツリーをクラウドマウント先へ単方向に同期する CLI。
> 頻繁に rewind する AI エージェント向けに作りました。

[![Go 1.22+](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go)](https://go.dev) [![macOS · Windows](https://img.shields.io/badge/platforms-macOS%20%7C%20Windows-lightgrey)]() [![status: MVP](https://img.shields.io/badge/status-MVP-blue)]()

🌐 [English](README.md) ・ **日本語**

`gdsync` は Git のワーキングツリーを、指定フォルダ（典型的には OneDrive / Google Drive / Dropbox / iCloud のローカルマウント）へ単方向にミラーリングするシングルバイナリ CLI です。リアルタイム監視、`.gitignore` 尊重、クラウド由来のファイルロックをリトライで吸収。一番の特徴は、`git reset --hard` でツリーが縮むと、クラウドフォルダも同じだけ縮むこと。

---

## 解きたい問題

AI コーディングエージェント（Claude Code, Cursor, Aider）は素早く編集し、頻繁に取り消します。プロジェクトがクラウド同期フォルダにあると、2 つが破綻します。

1. **クラウドクライアントは自分のスナップショットと比較する、Git ではない。** エージェントがファイルを削除した直後の rewind で一部が戻ると、クラウドの「削除済み」認識がズレ、復活・重複・conflict が発生します。
2. **意図と移動を区別できない。** `git reset --hard HEAD~5` で 40 ファイルが消えるのは、ユーザーが手で 40 ファイル消したのと見分けがつきません。OneDrive はある時は削除を反映し、ある時はファイルを「復元」します。

解決策はシンプルで、クラウドフォルダをワークスペースとして使うのをやめること。ミラーとして使い、Git から書き戻す。`gdsync` がやるのはそれだけです。

---

## できること

- FSEvents (macOS) / fsnotify (Linux, Windows) でワーキングツリーを監視
- `.gitignore` を尊重（ネスト対応） + ハードコード除外 (`.git/`, `.claude_code/`, `.cursor/`, `.DS_Store`, `*.tmp`, `node_modules/`)
- `temp + rename` のアトミック書き込み — クラウドクライアントが半端なファイルを見ることはない
- src で消えた瞬間、dst からも削除
- 30 秒ごと + Git の状態変化時 (`HEAD`, refs, packed-refs, index) に完全 reconcile を実行 = rewind 検知の核
- クラウドのファイルロックに対し指数バックオフでリトライ
- シンボリックリンクはスキップ（後述）

---

## アーキテクチャ

<p align="center">
  <img src="docs/architecture.svg" alt="gdsync architecture: one-way data flow from a Git working tree, through event watchers, a debounce/queue stage, and a backoff-wrapped sync worker, into a cloud-mounted destination" width="100%">
</p>

### Git を唯一の正にする

`gdsync` は単方向だけです。双方向同期はコンフリクト解決を要し、コンフリクト解決は authoritative な版を要し、それは結局 Git の再発明になります。

- `src` (Git ツリー) → `dst` (クラウドマウント)。例外なし。
- `dst` にあって `src` にないものは、次の reconcile で削除される。
- 編集は `src`、閲覧・共有は `dst`。正は常に片側だけ。

トレードオフ: `dst` で直接行った変更は失われます。このユースケースでは正しい判断です。

### クラウド由来のロックに指数バックオフで対応

クラウドクライアントは裏でファイルを open / hash / re-upload します。その間 `open()` や `rename()` は以下のエラーで失敗することがあります。

- POSIX: `EAGAIN`, `EBUSY`, `ETXTBSY`, `EACCES`
- Windows: `ERROR_SHARING_VIOLATION`, `ERROR_LOCK_VIOLATION`, `ERROR_ACCESS_DENIED`, `ERROR_CLOUD_FILE_IN_USE`

`gdsync` は指数バックオフでリトライします（100 ms → 30 s、最大 8 回、ジッタ付き）。リトライ不可なエラー (`ENOENT`, `ENOSPC` 等) は即座に失敗させます。

書き込みはアトミック。dst と同じディレクトリに temp ファイルを作り（`os.TempDir()` は使わない — クロスボリュームを避けるため）、`fsync` の後 rename。Windows は `MoveFileEx` + `MOVEFILE_WRITE_THROUGH` を使うので、呼び出しが返る前にディスクへコミットされます。

### シンボリックリンクはスキップ

Windows でのシンボリックリンク作成には `SeCreateSymbolicLinkPrivilege` が必要で、ユーザープロセスは通常持っていません。Windows で静かに失敗する機能を出すより、全プラットフォームで一律スキップします。src のリンクは walk 時点で無視、何らかの理由で dst に紛れ込んだリンクは孤児扱いで削除されます。

### Size + mtime で比較（ハッシュは使わない）

Reconciler は `(size, mtime)` で差分判定します。コピーごとに `Chtimes` で `dst.mtime = src.mtime` を設定するので、安定状態では 100 ms の許容範囲で一致します。OneDrive がアップロード後に `dst.mtime` を未来方向にずらすことがありますが、比較は `src.mtime > dst.mtime + skew` のみトリガするので問題ありません。コンテンツハッシュは毎回全バイトを舐めるので採用しません — rewind 検知に必要ありません。

### プラットフォーム別の監視

- **macOS**: `github.com/fsnotify/fsevents`。ネイティブで再帰、FD も省エネ。
- **Linux / Windows**: `github.com/fsnotify/fsnotify`。起動時に再帰下降して登録。新規ディレクトリの Create イベント時には watcher を追加し、登録前に存在した子要素には synthetic Create を発火（fsnotify 既知の race を回避）。

パスごとの 1 秒デバウンスで、エディタの write-temp + rename + fsync を 1 アクションに集約します。

### Git の状態監視

第 2 の watcher が `.git/HEAD`, `refs/heads/`, `packed-refs`, `index` を追跡。変化時に即時 reconcile を起こします。これにより：

- ブランチ切り替え (`git checkout`)
- ハードリセット (`git reset --hard`, HEAD は動かず ref だけ動くケース)
- インデックスのみの操作 (`git checkout -- file`)
- GC 後の ref (`packed-refs` 配下)

これがないと `--interval` 秒待つことになります。あればミリ秒で追従します。

---

## インストール

### go install

```sh
go install git-drive-sync/cmd/gdsync@latest
```

### ソースから

```sh
git clone <this repo> && cd git-drive-sync
make build           # → ./gdsync
make install         # → $GOBIN/gdsync
make dist            # → bin/gdsync-{darwin-arm64,darwin-amd64,windows-amd64.exe}
```

### ビルド済みバイナリ

`make dist` で macOS (arm64 / amd64) と Windows (amd64) のバイナリを生成。`PATH` に置けば完了。

ソースビルドは Go 1.22+。ビルド済みバイナリにランタイム依存なし。

---

## 使い方

```sh
cd ~/Code/my-project
gdsync --dest ~/OneDrive/my-project
```

これだけ。`gdsync` が `.gitignore` を読み、対象ファイルをコピーし、その後は永続的に監視します。`Ctrl-C` で最終 reconcile を流して終了。

### フラグ

| Flag | デフォルト | 効果 |
|---|---|---|
| `--dest` | 必須 | 同期先ディレクトリ（存在しなければ作成） |
| `--interval` | `30s` | 完全 reconcile の間隔 |
| `--debounce` | `1s` | パスごとのイベントデバウンス窓 |
| `--dry-run` | `false` | ログのみ、書き込みなし |
| `-v`, `--verbose` | `false` | DEBUG ログ |
| `--once` | `false` | reconcile を 1 回流して終了 |
| `--max-retries` | `8` | ファイルロック時のリトライ上限 |

### レシピ

ワンショット（cron 向け）：

```sh
gdsync --dest ~/OneDrive/my-project --once
```

`.gitignore` を調整しながら dry-run：

```sh
gdsync --dest /tmp/inspect --dry-run -v
```

macOS でバックグラウンド常駐：

```sh
nohup gdsync --dest ~/OneDrive/my-project > ~/Library/Logs/gdsync.log 2>&1 &
```

---

## 範囲外

- **双方向同期** — 「Git を唯一の正にする」の通り。永久に追加しません。
- **1 プロセスで複数 dst** — dst ごとに 1 プロセス。
- **サービスインストーラ (`launchd` / `systemd`)** — OS のサービスマネージャを利用。
- **設定ファイル** — 現状フラグのみ。
- **コンフリクト解決** — そもそも発生しません。

---

## レイアウト

```
gdsync/
├── cmd/gdsync/main.go          # cobra エントリ、イベント / ディスパッチループ
└── internal/
    ├── config/                 # フラグパース、パス正規化
    ├── gitignore/              # マッチャ（go-git のパーサ）
    ├── watcher/                # FSEvents + fsnotify
    ├── sync/                   # backoff, retry, atomic copy, reconcile
    ├── gitstate/               # HEAD / refs / index ウォッチャ
    └── log/                    # slog ラッパー
```

dst 書き込みは単一の mutex でシリアライズ。reconciler とイベントハンドラが race することはありません。

---

## テスト

```sh
go test ./...
make vet
```

E2E スモークテスト：

```sh
SRC=$(mktemp -d) && DST=$(mktemp -d) && cd $SRC && git init -q
echo "*.log" > .gitignore
echo hello > a.txt && echo ignored > debug.log
gdsync --dest $DST --once -v
ls $DST                          # → a.txt, .gitignore
rm a.txt
gdsync --dest $DST --once -v
ls $DST                          # → .gitignore のみ（rewind が伝播）
```

---

## ライセンス

[MIT](LICENSE) © 2026 Luca Minagawa

---

## 利用 OSS

- [`spf13/cobra`](https://github.com/spf13/cobra) — CLI フレームワーク
- [`fsnotify/fsnotify`](https://github.com/fsnotify/fsnotify) + [`fsnotify/fsevents`](https://github.com/fsnotify/fsevents) — ファイルシステムイベント
- [`go-git/go-git`](https://github.com/go-git/go-git) — ネスト `.gitignore` を正しく扱える gitignore パーサ
- [`golang.org/x/sys/windows`](https://pkg.go.dev/golang.org/x/sys/windows) — `MoveFileEx`
