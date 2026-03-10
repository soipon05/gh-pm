package cmd

import (
	"errors"
	"os"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/soipon05/gh-pm/internal/config"
	"github.com/spf13/cobra"
)

// appConfig は読み込み済みの設定。サブコマンドから参照する。
var appConfig *config.Config

// configPath は --config フラグで指定された設定ファイルパス。
var configPath string

// rootCmd はすべてのサブコマンドの親コマンド。
// `gh pm` を実行するとこのコマンドが動く。
var rootCmd = &cobra.Command{
	Use:   "pm",
	Short: "GitHub Projects のチーム別進捗を可視化する",
	Long: `gh-pm は GitHub Projects v2 のプロジェクト健全性を AI が構造的に把握するためのデータ取得レイヤーとして機能する gh CLI 拡張です。

使い方:
  gh pm report              # 全チームの進捗サマリー
  gh pm report backend      # backend チームの詳細
  gh pm report --format json # AI 分析用 JSON（diagnostics 付き）
  gh pm alert               # diagnostics シグナルベースのアラート
  gh pm snapshot            # 現在の状態を時系列データとして保存
  gh pm analyze             # スナップショット履歴からトレンド分析`,

	// PersistentPreRunE はすべてのサブコマンドが実行される前に必ず呼ばれる。
	// 認証チェックと設定ファイル読み込みをここで行う。
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := checkAuth(); err != nil {
			return err
		}

		// init コマンドは .gpm.yml がない状態で実行するので、config 読み込みをスキップ
		if cmd.Name() == "init" {
			return nil
		}

		return loadConfig()
	},
}

// Execute はエントリーポイントから呼ばれる唯一の関数。
// Cobra がコマンドライン引数を解析して適切なサブコマンドを実行する。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// --config フラグ（全サブコマンド共通）
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "設定ファイルのパス（省略時は自動検出）")

	// サブコマンドをルートに登録する
	rootCmd.AddCommand(newReportCmd())
	rootCmd.AddCommand(newAlertCmd())
	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newSnapshotCmd())
	rootCmd.AddCommand(newAnalyzeCmd())
	rootCmd.AddCommand(newStandupCmd())
	rootCmd.AddCommand(newRetroCmd())
}

// loadConfig は設定ファイルを読み込んで appConfig にセットする。
func loadConfig() error {
	path := configPath

	if path == "" {
		var err error
		path, err = config.FindConfigPath()
		if err != nil {
			return err
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	appConfig = cfg
	return nil
}

// checkAuth は gh auth login が完了しているかを確認する。
// go-gh ライブラリが gh の認証情報を読み取ってくれる。
func checkAuth() error {
	host, _ := auth.DefaultHost()
	token, _ := auth.TokenForHost(host)
	if token == "" {
		return errors.New("認証が必要です。`gh auth login` を実行してください")
	}
	return nil
}
