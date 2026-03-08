# Contributing to gh-pm

## 開発環境のセットアップ

```bash
# Go のインストール（mise 推奨）
mise use go@latest

# リポジトリのクローン
git clone https://github.com/soipon05/gh-pm
cd gh-pm

# 依存ライブラリのインストール
go mod download
```

## ビルドとテスト

```bash
# ビルド
go build -o gh-pm .

# テスト
go test ./...

# 静的解析
go vet ./...
```

## ローカルで gh extension として動作確認する

```bash
# gh-pm リポジトリのディレクトリで実行
gh extension install .

# 動作確認
gh pm report
```

## プルリクエストのガイドライン

- `main` ブランチに対して PR を作成してください
- PR を出す前に `go test ./...` と `go vet ./...` が通ることを確認してください
- コミットメッセージは変更内容が分かる日本語または英語で書いてください

## ディレクトリ構成

```
gh-pm/
├── cmd/            # cobra コマンド定義
├── internal/
│   ├── analytics/  # 診断シグナルの計算ロジック
│   ├── config/     # .gpm.yml の読み込み
│   ├── github/     # GitHub GraphQL API クライアント
│   └── render/     # テーブル・JSON・アラートの出力
└── testdata/       # テスト用フィクスチャ
```

## ライセンス

コントリビュートされたコードは MIT ライセンスのもとで公開されます。
