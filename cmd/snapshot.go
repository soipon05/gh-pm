package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/soipon05/gh-pm/internal/analytics"
	"github.com/soipon05/gh-pm/internal/config"
	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/soipon05/gh-pm/internal/render"
	"github.com/spf13/cobra"
)

const snapshotDir = ".gpm-history"

func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "現在のプロジェクト状態を時系列データとして保存する",
		Long: `現在のプロジェクト状態をスナップショットとして保存する。
振り返りやトレンド分析（gh pm analyze）の基盤データとなる。

保存先: .gpm-history/YYYY-MM-DD.json
保存内容: gh pm report --format json と同じ構造（diagnostics 付き）

cron や GitHub Actions で定期実行することを推奨。`,
		Example: `  gh pm snapshot    # 手動で1回保存`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshot(appConfig)
		},
	}

	return cmd
}

// runSnapshot はスナップショット保存の実際の処理。
func runSnapshot(cfg *config.Config) error {
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

	// 3. Diagnostics 計算
	th := analytics.Thresholds{
		WIPPerPerson:      cfg.Alerts.WIPPerPerson,
		AnomalyPercentile: cfg.Alerts.AnomalyPercentile,
		ReviewBounce:      cfg.Alerts.ReviewBounce,
	}
	mapper := func(s string) string { return cfg.CategoryOf(s) }
	diag, err := analytics.ComputeAll(items, th, mapper)
	if err != nil {
		return fmt.Errorf("診断シグナルの計算に失敗しました: %w", err)
	}
	hint := analytics.GenerateHint(diag, th)

	// 4. JSON を生成
	report := render.BuildReportJSON(items, diag, hint, cfg.Project.Owner, cfg.Project.Number, "", nil)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON の生成に失敗しました: %w", err)
	}

	// 5. ディレクトリ作成 + ファイル保存
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("ディレクトリの作成に失敗しました: %w", err)
	}

	filename := time.Now().Format("2006-01-02") + ".json"
	path := filepath.Join(snapshotDir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("スナップショットの保存に失敗しました: %w", err)
	}

	fmt.Printf("スナップショットを保存しました: %s\n", path)
	return nil
}
