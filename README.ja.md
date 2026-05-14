# gdsync — `git-drive-sync`

> **AI 駆動開発時代のための、Git ワーキングツリーからクラウドマウント先への単方向同期 CLI。**
>
> Git が真実。クラウドはその鏡。エージェントが rewind すれば、クラウドも rewind する。

[![Go 1.22+](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go)](https://go.dev) [![macOS · Windows](https://img.shields.io/badge/platforms-macOS%20%7C%20Windows-lightgrey)]() [![status: MVP](https://img.shields.io/badge/status-MVP-blue)]()

🌐 [English](README.md) ・ **日本語**

`gdsync` は、Git ワーキングツリーを **片方向に** ローカルファイルシステム上の指定ディレクトリ — 典型的には OneDrive / Google Drive / Dropbox / iCloud Drive のローカルマウント — へミラーリングするシングルバイナリ CLI です。ツリーをリアルタイムに監視し、`.gitignore` を尊重し、クラウド同期クライアントが頻繁に投げてくる一時的なファイルロックエラーをリトライで吸収します。そして何より、**rewind を検知します**: AI エージェントが `git reset --hard` や独自の `/rewind` を実行してワーキングツリーが縮むと、クラウドフォルダも同じだけ縮みます。

---

## なぜ作ったか

近年の AI コーディングエージェント（Claude Code, Cursor, Aider など）は、ワーキングツリーを猛烈な勢いで編集します — 数十ファイルが書き込まれ、削除され、リファクタリングされ、そして時には丸ごと取り消されます。プロジェクトがクラウド同期フォルダ配下にある場合、このワークロードで 2 つの問題が顕在化します。

1. **クラウド同期クライアントは双方向で stateful です。** Git ツリーではなく *自分が最後に見たスナップショット* との差分で動きます。エージェントがファイルを削除した直後に rewind が一部を戻すと、クラウド側の「削除された」という認識が壊れ、ファイルが残留したり、ゴーストが復活したり、conflict copy が生まれたりします。
2. **「ユーザーが消した」と「タイムラインが動いたから消えた」を区別できません。** `git reset --hard HEAD~5` で 40 個の追跡外/staged ファイルが消えるのは、OneDrive から見れば 40 件の削除イベントに見えます。クライアントは半分の確率でその削除をクラウドに反映し、もう半分の確率で「何かおかしい」と判断してファイルをディスクに「復元」してしまいます。

結果として、Git が「こうあるべき」と言っている状態からプロジェクトディレクトリが乖離し、次のエージェントが再び編集対象としてしまう不要な遺物で汚染されます。治療法は単純で、**クラウドフォルダをワークスペースとして扱うのをやめる** こと。代わりにそれをミラーとして扱い、すでに意図を表現できるソース・オブ・トゥルース — Git ワーキングツリー — から継続的に書き戻す。

`gdsync` はそのミラーです。

---

## 何をするか

- **監視**: FSEvents (macOS) または fsnotify (Linux / Windows) で Git ワーキングツリーを監視。
- **フィルタ**: 全イベントをリポジトリの `.gitignore` （ネスト `.gitignore` セマンティクスは Git 本体と同等）と、ハードコードされた除外セット `.git/` / `.claude_code/` / `.cursor/` / `.DS_Store` / `*.tmp` / `node_modules/` を通してふるい分け。
- **コピー**: 変更されたファイルを `temp → rename` のアトミック書き込みで `--dest` へコピー。半端な状態のファイルがクラウドクライアントから見えることはありません。
- **削除**: ソースから消えた瞬間に `--dest` からも消します。
- **Reconcile**: 30 秒ごと、および Git の状態変化時 (`.git/HEAD`, refs, packed-refs, index) に src/dst の完全比較を実行。これが rewind 検知の核です — `src` から消えたファイルは例外なく `dst` からも消えます。
- **リトライ**: クラウドクライアントが一時的に dst をロックした場合、指数バックオフで再試行。
- **シンボリックリンク**: 設計上、完全にスキップ（後述）。

---

## アーキテクチャと設計上の判断

<p align="center">
  <img src="docs/architecture.svg" alt="gdsync architecture: one-way data flow from a Git working tree, through event watchers, a debounce/queue stage, and a backoff-wrapped sync worker, into a cloud-mounted destination" width="100%">
</p>

### Git を唯一のソース・オブ・トゥルースに

`gdsync` は意図して単方向です。双方向同期は新しい問題のクラス — コンフリクト解決 — を生みます。コンフリクトリゾルバを持った瞬間、authoritative な版を持つことになり、authoritative な版を持つということは Git を再発明することになります。よってモデルはずっとシンプルに：

- **`src` (Git ワーキングツリー) → `dst` (クラウドマウント)**. 常に。例外なく。
- `dst` に存在して `src` に存在しないものは、定義上 *誤り* であり、次の reconcile で削除されます。
- ユーザーは `src` でファイルを編集し、`dst` 経由で読む / 共有する。どちらが正かを考える必要がありません — 正は常に一方だけだからです。

これにより、マージコンフリクト、勝敗判定、クラウドクライアントの解釈、といった面倒の全てが消えます。コストは現実的に存在します — 同期先フォルダ内で直接行った変更は失われます。それはこのツールの取引であり、ユースケースに対して正しい取引です。

### 堅牢な I/O: クラウド由来のロックに対する指数バックオフ

クラウド同期クライアントは「気になる」ファイルを積極的に open / hash / re-upload します。その間、dst に対する `open()` や `rename()` は以下のエラーで失敗することがあります：

- POSIX: `EAGAIN`, `EBUSY`, `ETXTBSY`, `EACCES`
- Windows: `ERROR_SHARING_VIOLATION` (32), `ERROR_LOCK_VIOLATION` (33), `ERROR_ACCESS_DENIED` (5), `ERROR_CLOUD_FILE_IN_USE` (0x80070189)

いずれも一時的なものです。`gdsync` は dst 側の全 I/O を分類器駆動のリトライでラップします — エラーをリトライ可能と識別し、バックオフし (100 ms → 200 ms → 400 ms → … 最大 30 s、ジッタ付き、最大 8 回)、再試行。リトライ不可能なエラー (`ENOENT`, `ENOSPC` 等) は即座に失敗させて可視化します。

書き込みはアトミックです: temp ファイルを **dst と同じディレクトリ内** に作成し（`os.TempDir()` は使いません — クロスボリュームになるため）、`fsync` して、最終パスに rename。Windows では `golang.org/x/sys/windows` 経由で `MoveFileEx(... | MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)` を呼ぶので、呼び出しが返る前にディスクへコミットされ、クラウドクライアントが部分書き込み状態を観測することはありません。

### シンボリックリンク: 意図的にスキップ

Windows でのシンボリックリンクは `SeCreateSymbolicLinkPrivilege` が必要ですが、ユーザープロセスは通常これを持ちません。「ベストエフォート」のシンボリックリンク複製を入れると、2 番目に重要なターゲットプラットフォームで静かに劣化する機能を出荷することになり、加えてソース側でシンボリックリンクループの問題も招きます。`gdsync` はこのバグのクラス全体を拒否します — `src` のシンボリックリンクは walk 時点でスキップされ、何らかの理由で `dst` に紛れ込んだシンボリックリンクは「対応する `src` がない」とみなされ次の reconcile で削除されます。macOS と Windows で挙動が完全に一致する、というのがこの選択の主旨です。

### Mtime + size を使う（コンテンツハッシュは使わない）

Reconciler はファイルを `(size, mtime)` で比較します。コピーごとに `Chtimes` で明示的に `dst.mtime = src.mtime` を設定するので、安定状態では両者が一致する想定です — ファイルシステム解像度のずれを吸収するために 100 ms の許容範囲を入れています。OneDrive がアップロード後に `dst.mtime` を *未来方向に* ずらすクセは、比較の向きで吸収されます（`src.mtime > dst.mtime + skew` のときだけコピーをトリガするため）。コンテンツハッシュを採用しなかった理由：毎回全バイトを線形にスキャンする上、rewind 検知ロジックは中身を見る必要がないからです — エントリが消えているかどうかは、1 バイトのファイルでも 1 GB のファイルでも同じ判定です。

### プラットフォーム別の監視

- **macOS** は `github.com/fsnotify/fsevents` を使用。FSEvents はネイティブで再帰的、ファイル記述子も省エネ — ディレクトリ 5 万個のリポジトリでも実質コストゼロで監視できます。
- **Linux / Windows** は `github.com/fsnotify/fsnotify` を、再帰下降を自前で行う形で使用。Create イベント時には新しいサブツリーを直ちに walk して登録し、watcher が attach する前に存在した子要素に対しては synthetic Create イベントを生成します（fsnotify でよく知られた race を回避）。

パスごとのデバウンス（デフォルト 1 秒）が、エディタの保存ダンス — write-temp + rename + fsync を場合によっては複数回 — を、最終ファイル名でキーされた 1 つの同期アクションに集約します。

### Git 状態の監視

第 2 の watcher が `.git/HEAD`, `.git/refs/heads/`, `.git/packed-refs`, `.git/index` を追跡し、変化があれば即座に reconcile を起こします。これにより以下が捕捉されます：

- ブランチ切り替え (`git checkout`)
- 現在ブランチに対するハードリセット (`git reset --hard`、HEAD は動かないが ref が動く)
- インデックスのみの操作 (`git checkout -- file`)
- GC された ref (`packed-refs` 配下)

これがないと、定期 reconcile が気付くまで最大 `--interval` 秒待つことになります。これがあれば、Git 操作完了からミリ秒オーダーでクラウドが追従します。

---

## インストール

### `go install` (推奨)

```sh
go install git-drive-sync/cmd/gdsync@latest
```

`gdsync` バイナリが `$(go env GOBIN)` （デフォルトは `$HOME/go/bin`）に配置されます。

### ソースからビルド

```sh
git clone <this repo> && cd git-drive-sync
make build           # → ./gdsync
make install         # → $GOBIN/gdsync
make dist            # → bin/gdsync-{darwin-arm64,darwin-amd64,windows-amd64.exe}
```

### ビルド済みバイナリ

`make dist` で macOS (arm64 + amd64) と Windows (amd64) のクロスコンパイル済みバイナリが得られます。`PATH` に置けば完了 — ランタイムもデーモンもサービス登録も不要です。

**要件**: ソースビルドには Go 1.22+。ビルド済みバイナリにランタイム依存なし。

---

## 使い方

### Zero Config

```sh
# 1. Git リポジトリに入る
cd ~/Code/my-project

# 2. ミラー先のクラウドマウントディレクトリを指定
gdsync --dest ~/OneDrive/my-project
```

これだけです。`gdsync` は `.gitignore` を読み、対象ファイルをコピーし、その後は永続的に監視します。`Ctrl-C` で最終 reconcile を流して終了します。

### よく使うフラグ

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--dest` | *(必須)* | 同期先ディレクトリ（例: OneDrive マウント）。存在しない場合は作成。 |
| `--interval` | `30s` | 完全 reconcile の実行間隔。 |
| `--debounce` | `1s` | 同一パスの後続イベントをまとめる窓。 |
| `--dry-run` | `false` | 書き込みせずログ出力のみ。 |
| `-v`, `--verbose` | `false` | DEBUG レベルログ（ファイル単位の copy/delete を表示）。 |
| `--once` | `false` | 1 回の完全 reconcile を実行して終了。cron / pre-commit hook 向け。 |
| `--max-retries` | `8` | クラウド由来のファイルロックに対する最大リトライ回数。 |

### レシピ

**ワンショット同期（watcher なし）** — `cron` 用途に：

```sh
gdsync --dest ~/OneDrive/my-project --once
```

**`.gitignore` を調整しながら dry-run で監視**:

```sh
gdsync --dest /tmp/inspect --dry-run -v
```

**macOS でバックグラウンドミラーとして実行**（`gdsync install` 実装までの暫定）：

```sh
nohup gdsync --dest ~/OneDrive/my-project > ~/Library/Logs/gdsync.log 2>&1 &
```

---

## 非スコープ（MVP 段階で意図的に外したもの）

設計上、MVP は表面積を小さく保ちます。以下は明示的に範囲外です：

- **双方向同期** — 上記「Git を唯一のソース・オブ・トゥルースに」の通り。永久に追加しません。
- **1 プロセスで複数 dst** — 当面は dst ごとに `gdsync` を 1 つ起動してください。
- **`gdsync install` / launchd / systemd 連携** — `nohup` か OS のサービスマネージャーをご利用ください。
- **設定ファイル (`.gdsync.yaml`)** — 当面フラグのみ。
- **コンフリクト解決 UI / Web ダッシュボード** — そもそもコンフリクトが存在しません。

---

## プロジェクト構成

```
gdsync/
├── cmd/gdsync/main.go          # cobra エントリポイント、イベント / ディスパッチループ
└── internal/
    ├── config/                 # フラグパース、パス正規化
    ├── gitignore/              # マッチャ（go-git の gitignore パーサを使用）
    ├── watcher/                # FSEvents (darwin) + fsnotify (others)
    ├── sync/                   # backoff, retry classification, atomic copy, reconcile
    ├── gitstate/               # HEAD / refs / index ウォッチャ
    └── log/                    # slog ラッパー（verbose 切り替え付き）
```

全体フロー：

```
   fsnotify/FSEvents ─┐
                     ├─▶  debounce  ──▶  serialized sync worker  ──▶  dst
   git state watch  ─┤                                  ▲
                     │                                  │
   periodic ticker  ─┴──────────▶  reconcile  ──────────┘
```

dst 書き込みは単一の mutex でシリアライズされるため、reconciler とライブイベントハンドラが race することはありません。

---

## テスト

```sh
go test ./...        # gitignore matcher / backoff retry / syncer のユニットテスト
make vet             # go vet ./...
```

バイナリの E2E スモークテスト：

```sh
# 初期同期、.gitignore の効力、ハードコード除外、rewind 検知
SRC=$(mktemp -d) && DST=$(mktemp -d) && cd $SRC && git init -q
echo "*.log" > .gitignore
echo hello > a.txt && echo ignored > debug.log
gdsync --dest $DST --once -v
ls $DST                          # → a.txt, .gitignore
rm a.txt
gdsync --dest $DST --once -v
ls $DST                          # → .gitignore のみ（rewind が伝播した）
```

---

## ライセンス

[MIT](LICENSE) © 2026 Hikaru Minagawa

---

## 謝辞

以下の OSS の上に成り立っています：

- [`spf13/cobra`](https://github.com/spf13/cobra) — CLI フレームワーク
- [`fsnotify/fsnotify`](https://github.com/fsnotify/fsnotify) と [`fsnotify/fsevents`](https://github.com/fsnotify/fsevents) — ファイルシステムイベント配信
- [`go-git/go-git`](https://github.com/go-git/go-git) — Go エコシステムで唯一、ネスト `.gitignore` と否定パターンを正しく扱える gitignore マッチャ
- [`golang.org/x/sys/windows`](https://pkg.go.dev/golang.org/x/sys/windows) — `MoveFileEx` と Win32 エラーカタログ
