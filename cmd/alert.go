package cmd

import (
	"fmt"

	"github.com/soipon05/gh-pm/internal/analytics"
	"github.com/soipon05/gh-pm/internal/config"
	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/soipon05/gh-pm/internal/render"
	"github.com/spf13/cobra"
)

// alertFlags は `gh pm alert` コマンドのフラグをまとめた構造体。
type alertFlags struct {
	team   string // チーム絞り込み（空文字 = 全チーム）
	format string // "table" | "json"
}

func newAlertCmd() *cobra.Command {
	flags := &alertFlags{}

	cmd := &cobra.Command{
		Use:   "alert",
		Short: "diagnostics シグナルベースのアラートを表示する",
		Long: `diagnostics シグナルに基づいてプロジェクトの問題を検出・表示する。

アラートトリガー:
  - 異常値: p85 超過（Vacanti: ソフトウェアメトリクスは正規分布しない）
  - WIP 過多: 1人あたり > 2（Little's Law: WIP増 → サイクルタイム増）
  - レビュー滞留: In Review >= In Progress（ToC: ボトルネック検出）
  - 完了ゼロ: 直近7日 Done = 0（スループット停止）
  - 差し戻しループ: bounce >= 2（レビュープロセスの問題）`,
		Example: `  gh pm alert                     # 全シグナルでチェック
  gh pm alert --team backend      # backend チームだけ
  gh pm alert --format json       # JSON 出力（GitHub Actions 連携用）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlert(appConfig, flags)
		},
	}

	// フラグの登録
	cmd.Flags().StringVar(&flags.team, "team", "", "チーム名で絞り込む")
	cmd.Flags().StringVar(&flags.format, "format", "table", "出力形式: table|json")

	return cmd
}

// runAlert はアラート表示の実際の処理。
func runAlert(cfg *config.Config, flags *alertFlags) error {
	// 1. GitHub API からアイテムを取得
	client, err := gh.NewClient()
	if err != nil {
		return err
	}

	items, err := client.ListProjectItems(cfg.Project.Owner, cfg.Project.Number, cfg.Fields.Status.Name)
	if err != nil {
		return err
	}

	// 2. StatusCategory をマッピング
	for i := range items {
		items[i].StatusCategory = cfg.CategoryOf(items[i].Status)
	}

	// 3. チーム絞り込み
	filtered := items
	if flags.team != "" {
		if _, ok := cfg.Teams[flags.team]; !ok {
			return fmt.Errorf("チーム %q が設定ファイルに見つかりません", flags.team)
		}
		filtered = filterByTeam(items, cfg, flags.team)
	}

	// 4. Diagnostics 計算
	th := analytics.Thresholds{
		WIPPerPerson:      cfg.Alerts.WIPPerPerson,
		AnomalyPercentile: cfg.Alerts.AnomalyPercentile,
		ReviewBounce:      cfg.Alerts.ReviewBounce,
	}
	mapper := func(s string) string { return cfg.CategoryOf(s) }
	diag, err := analytics.ComputeAll(filtered, th, mapper)
	if err != nil {
		return fmt.Errorf("診断シグナルの計算に失敗しました: %w", err)
	}

	// 5. アラート生成
	alerts := render.GenerateAlerts(diag, th)

	// 6. 出力
	switch flags.format {
	case "json":
		return render.PrintAlertJSON(alerts)
	default:
		render.PrintAlertTable(alerts, false)
		return nil
	}
}
