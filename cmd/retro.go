package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/soipon05/gh-pm/internal/analytics"
	"github.com/soipon05/gh-pm/internal/config"
	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/soipon05/gh-pm/internal/render"
	"github.com/spf13/cobra"
)

type retroFlags struct {
	sprintDays int
	noColor    bool
	refresh    bool
}

func newRetroCmd() *cobra.Command {
	flags := &retroFlags{}

	cmd := &cobra.Command{
		Use:   "retro",
		Short: "スプリント振り返りを表示する",
		Long: `直近スプリントのスループット・サイクルタイム・ボトルネックを分析し、
SMARTなアクション提案を出力する。

スナップショット（.gpm-history/）がある場合は前回比較も表示する。
gh pm snapshot を定期実行（週1など）することで比較が可能になる。`,
		Example: `  gh pm retro                   # 直近2週間の振り返り
  gh pm retro --sprint-days 7   # 直近1週間
  gh pm retro --refresh         # 最新データで実行`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRetro(appConfig, flags)
		},
	}

	cmd.Flags().IntVar(&flags.sprintDays, "sprint-days", 14, "スプリント期間（日数）")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "カラー出力を無効化する")
	cmd.Flags().BoolVar(&flags.refresh, "refresh", false, "キャッシュを無視して最新データを取得")

	return cmd
}

func runRetro(cfg *config.Config, flags *retroFlags) error {
	var (
		client *gh.Client
		err    error
	)
	if flags.refresh {
		client, err = gh.NewClientWithRefresh()
	} else {
		client, err = gh.NewClient()
	}
	if err != nil {
		return err
	}

	fmt.Fprint(os.Stderr, "取得中...")

	// アイテム取得（全チームの高速パス）
	var items []gh.ProjectItem
	if members := allUniqueMembers(cfg); len(members) > 0 {
		items, _, err = client.ListTeamItems(
			cfg.Project.Owner, cfg.Project.Number,
			members, cfg.Fields.Status.Name,
		)
	} else {
		items, err = client.ListProjectItems(cfg.Project.Owner, cfg.Project.Number, cfg.Fields.Status.Name)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}
	fmt.Fprintln(os.Stderr, " 完了")

	// StatusCategory をマッピング
	for i := range items {
		items[i].StatusCategory = cfg.CategoryOf(items[i].Status)
	}

	// レベル参照関数
	levelOf := func(id string) string {
		return cfg.MemberLevelOf(id)
	}

	// RetroReport 計算
	report := analytics.ComputeRetro(
		items,
		flags.sprintDays,
		cfg.Alerts.WIPPerPerson,
		levelOf,
	)

	// スナップショットから前回データを読み込んで比較
	if prev := loadPrevSnapshot(cfg, flags.sprintDays); prev != nil {
		report.Prev = prev
		report.Throughput.PrevCount = prev.Throughput.DoneCount
		report.Throughput.ChangePercent = calcChangePercent(
			float64(report.Throughput.DoneCount),
			float64(prev.Throughput.DoneCount),
		)
		report.CycleTime.PrevMedian = prev.CycleTime.Median
		report.CycleTime.PrevP85 = prev.CycleTime.P85
	}

	// 表示
	render.PrintRetro(report, flags.noColor)

	return nil
}

// loadPrevSnapshot は直前のスナップショットを読み込んでRetroReportに変換する。
func loadPrevSnapshot(cfg *config.Config, sprintDays int) *analytics.RetroReport {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return nil
	}

	// 日付順にソートして、今日より古い最新のスナップショットを探す
	var snapFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			snapFiles = append(snapFiles, e.Name())
		}
	}
	sort.Strings(snapFiles) // YYYY-MM-DD.json の文字列ソート = 日付順

	today := time.Now().Format("2006-01-02")
	var targetFile string
	for i := len(snapFiles) - 1; i >= 0; i-- {
		name := snapFiles[i]
		dateStr := name[:len(name)-5] // ".json" を除去
		if dateStr < today {
			targetFile = name
			break
		}
	}

	if targetFile == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(snapshotDir, targetFile))
	if err != nil {
		return nil
	}

	// スナップショットのJSON構造から最低限のデータを取り出す
	var snap snapshotJSON
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil
	}

	// スナップショットのアイテムを gh.ProjectItem に変換して再計算
	var prevItems []gh.ProjectItem
	for _, item := range snap.Items {
		prevItems = append(prevItems, gh.ProjectItem{
			Number:         item.Number,
			Title:          item.Title,
			Assignees:      item.Assignees,
			Status:         item.Status,
			StatusCategory: item.StatusCategory,
			ElapsedDays:    item.ElapsedDays,
		})
	}

	levelOf := func(id string) string {
		return cfg.MemberLevelOf(id)
	}

	return analytics.ComputeRetro(prevItems, sprintDays, cfg.Alerts.WIPPerPerson, levelOf)
}

func calcChangePercent(now, prev float64) float64 {
	if prev == 0 {
		return 0
	}
	return (now - prev) / prev * 100
}

// snapshotJSON はスナップショットファイルの最低限の構造。
type snapshotJSON struct {
	Items []snapshotItem `json:"items"`
}

type snapshotItem struct {
	Number         int      `json:"number"`
	Title          string   `json:"title"`
	Assignees      []string `json:"assignees"`
	Status         string   `json:"status"`
	StatusCategory string   `json:"status_category"`
	ElapsedDays    int      `json:"elapsed_days"`
}
