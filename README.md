# gh-pm

GitHub Projects v2 のプロジェクト健全性を AI が構造的に把握するためのデータ取得レイヤーとして機能する gh CLI 拡張ツール。

```
gh pm report backend
```

```
=== プロジェクト進捗 (2026-03-05) ===

チーム      Todo  In Progress  In Review  Done(7d)
──────────────────────────────────────────────────
backend        1          4          3         2
frontend       2          3          2         1

合計:          3          7          5         3

─── backend チーム詳細 ────────────────────────
● In Progress
  #102 商品一覧API実装 (alice)              12d
  #301 CI/CDパイプライン構築 (charlie)      24d ▲

● In Review
  #104 カート機能実装 (alice)               17d ▲
  #203 商品詳細画面 (bob)                   20d ▲

● Todo
  #405 管理画面ダッシュボード (未アサイン)
```

## インストール

```bash
gh extension install <owner>/gh-pm
```

**前提条件**: `gh` CLI がインストールされ、`gh auth login` が完了していること。

## セットアップ

プロジェクトのルートで実行する。

```bash
gh pm init
```

対話形式で `.gpm.yml` が生成される。Status フィールドの値を API から自動検出し、カテゴリへのマッピングを対話的に設定する。

## コマンド

| コマンド | 説明 |
|---|---|
| `gh pm init` | 初回セットアップウィザード（Status マッピング + チーム定義） |
| `gh pm report` | 全チームの進捗サマリー |
| `gh pm report <team>` | チーム別の詳細表示 |
| `gh pm alert` | diagnostics シグナルベースのアラート表示 |
| `gh pm snapshot` | 現在の状態を `.gpm-history/` に保存 |
| `gh pm analyze` | スナップショット履歴を比較してトレンドを出力 |

### フラグ

```bash
# report
gh pm report --format json     # AI 分析用 JSON（diagnostics 付き）
gh pm report --format csv      # CSV 出力（スプレッドシート用）
gh pm report --no-color        # カラー出力を無効化

# alert
gh pm alert --team backend     # チーム絞り込み
gh pm alert --format json      # JSON 出力（GitHub Actions 連携用）

# analyze
gh pm analyze --days 30        # 30日間のトレンド
gh pm analyze --mode retro     # 振り返り用データ
gh pm analyze --mode standup   # 朝会用データ
```

## 設定ファイル（.gpm.yml）

プロジェクトルートに置く。`gh pm init` で自動生成される。

```yaml
project:
  owner: example-org
  number: 1

# Status フィールドのマッピング（init ウィザードが自動検出）
fields:
  status:
    name: Status
    values:
      todo: "Todo"
      in_progress: "In Progress"
      in_review: "In Review"
      done: "Done"

teams:
  backend:
    members:
      - alice
      - bob
  frontend:
    members:
      - charlie
      - diana

# アラート閾値（省略時はデフォルト値を使用）
# alerts:
#   wip_per_person: 3      # デフォルト: 2（Little's Law）
#   anomaly_percentile: 95 # デフォルト: 85（Vacanti）
#   review_bounce: 3       # デフォルト: 2
#   zero_done_days: 14     # デフォルト: 7
```

`.gpm.yml.example` をコピーして使うこともできる。

## Diagnostics + AI 連携

`--format json` 時、gh-pm は3層構造のデータを出力する:

1. **items[]** -- 各 issue の生データ
2. **diagnostics** -- 7つの事前計算済み診断シグナル（ボトルネック、WIP、フロー効率、レビューサイクル、負荷偏り、依存ブロック、異常値）
3. **ai_hint** -- ルールベースの因果仮説 + 推奨アクション

AI は生データを1から分析する代わりに、diagnostics を検証・深掘りするだけで根本原因を特定できる。

### Claude Code Skills

| Skill | 用途 |
|---|---|
| `flow-diagnose` | ToC + フローメトリクスでボトルネック診断 |
| `standup` | WIP ベースの今日やるべきこと提案 |
| `retro` | 因果ループ分析 + SMART アクション提案 |

## 表示スタイル

- ステータスヘッダ: `●` ドット + ターミナル色（`--no-color` 時は `*`）
- 経過日数: `12d` 形式
- アラート: `▲` 警告（黄）、`▲▲` 緊急（赤）

## ドキュメント

- [要件定義書](docs/requirements.md)
- [gh-pm とは何か](docs/what-is-gh-pm.md)
- [アーキテクチャ決定記録（ADR）](docs/adr.md)
- [類似 OSS 調査](docs/research.md)
- [AI 時代の失敗パターン調査](docs/research-failure-patterns.md)

## 開発

```bash
# Go のインストール（mise 推奨）
mise use go@latest

# 依存ライブラリのインストール
go mod download

# ビルド
go build -o gh-pm .

# テスト
go test ./...
```

## ライセンス

MIT
