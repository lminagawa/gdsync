# git-drive-sync (`gdsync`)

Git ワーキングツリーから、`.gitignore` で除外されないファイルだけを OneDrive / Google Drive 等のローカルマウントディレクトリへ**単方向ミラーリング**する常駐 CLI。AI エージェントによる `git reset` や rewind による「予期せぬファイル消失」も、定期 reconcile で同期先から確実に削除します。

## 特徴

- **Zero Config**: 実行ディレクトリの `.gitignore` を自動パース（nested gitignore 対応）
- **ハードコード除外**: `.git/`, `.claude_code/`, `.cursor/`, `.DS_Store`, `*.tmp`, `node_modules/`
- **OS パスベース**: API 不使用、純粋なファイル操作のみ
- **完全単方向**: クラウド側の変更は無視、ローカル Git ツリーが Source of Truth
- **ファイルロック対応**: OneDrive 等の一時ロックに対し指数バックオフでリトライ（最大 8 回 / 約 60 秒）
- **Rewind 検知**: 定期的に src/dst を完全比較し、削除を確実に伝播
- **シンボリックリンクはスキップ**: Windows の権限エラーや無限ループを完全に回避
- **シングルバイナリ**: Mac (arm64/amd64) / Windows (amd64) クロスコンパイル対応

## ビルド

```sh
brew install go         # Go 1.22+
go mod tidy
make build              # ./gdsync が生成される
make install            # $GOBIN にインストール
make dist               # bin/ にクロスコンパイル成果物
```

## 使い方

```sh
# Git リポジトリのルートで実行
gdsync --dest "/Users/me/OneDrive/sync-here"

# 詳細ログ
gdsync --dest "/path" -v

# 一回だけ完全同期して終了（cron 用）
gdsync --dest "/path" --once

# 試しに見るだけ
gdsync --dest "/path" --dry-run
```

### フラグ

| フラグ | デフォルト | 説明 |
|---|---|---|
| `--dest` | （必須） | 同期先ディレクトリ |
| `--interval` | `30s` | 定期 reconcile 間隔 |
| `--debounce` | `1s` | fsnotify イベントのデバウンス窓 |
| `--dry-run` | `false` | 書き込みせずログ出力のみ |
| `-v, --verbose` | `false` | DEBUG レベルログ |
| `--once` | `false` | 起動時 reconcile のみで終了 |
| `--max-retries` | `8` | ファイルロック検出時の最大リトライ回数 |

## 動作の概要

1. 起動時に完全 reconcile (src → dst) を実行
2. fsnotify (Win/Linux) または FSEvents (macOS) でファイル変更を検知
3. `.gitignore` チェック後、対象ファイルを `temp + rename` でアトミックにコピー
4. ファイル削除も即時反映
5. `.git/HEAD`, `refs/`, `packed-refs`, `index` 変化時は即時 reconcile（`git reset` 等を捕捉）
6. 30 秒ごとに定期 reconcile（fsnotify 取りこぼし対策）
7. SIGINT/SIGTERM 受信時は最終 reconcile を流して終了

## 既知の制約

- シンボリックリンクは同期対象外
- 双方向同期は非対応（設計上）
- 単一 `--dest` のみ（MVP）

## ライセンス

MIT (LICENSE 未追加)
