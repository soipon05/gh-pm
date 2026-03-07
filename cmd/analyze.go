package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/soipon05/gh-pm/internal/config"
	"github.com/soipon05/gh-pm/internal/render"
	"github.com/spf13/cobra"
)

// analyzeFlags は `gh pm analyze` コマンドのフラグをまとめた構造体。
type analyzeFlags struct {
	days int    // 分析期間（日数）
	mode string // "trend" | "retro" | "standup"
}

func newAnalyzeCmd() *cobra.Command {
	flags := &analyzeFlags{}

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "スナップショット履歴からトレンドを分析する",
		Long: `スナップショット履歴を比較してトレンドを出力する。
AI Skills（flow-diagnose, standup, retro）の入力データとなる。

モード:
  trend   - WIP/スループット/サイクルタイムの推移（デフォルト）
  retro   - 振り返り用データ（因果ループ分析向け）
  standup - 朝会用データ（今日やるべきこと提案向け）`,
		Example: `  gh pm analyze                  # 直近7日間のトレンド
  gh pm analyze --days 30        # 30日間のトレンド
  gh pm analyze --mode retro     # 振り返り用データ
  gh pm analyze --mode standup   # 朝会用データ`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze(appConfig, flags)
		},
	}

	// フラグの登録
	cmd.Flags().IntVar(&flags.days, "days", 7, "分析期間（日数）")
	cmd.Flags().StringVar(&flags.mode, "mode", "trend", "分析モード: trend|retro|standup")

	return cmd
}

// --- analyze 出力構造 ---

type analyzeOutput struct {
	Period    periodRange     `json:"period"`
	Mode      string          `json:"mode"`
	Trend     *trendData      `json:"trend,omitempty"`
	Snapshots []snapshotEntry `json:"snapshots"`
}

type periodRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type trendData struct {
	WIPChange          string `json:"wip_change"`
	ThroughputChange   string `json:"throughput_change"`
	AvgCycleTimeChange string `json:"avg_cycle_time_change,omitempty"`
}

type snapshotEntry struct {
	Date    string         `json:"date"`
	Summary snapshotSummary `json:"summary"`
}

type snapshotSummary struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
	WIP      int            `json:"wip"`
}

// runAnalyze はトレンド分析の実際の処理。
func runAnalyze(cfg *config.Config, flags *analyzeFlags) error {
	// 1. スナップショットファイルを読み込み
	snapshots, err := loadSnapshots(flags.days)
	if err != nil {
		return err
	}

	if len(snapshots) == 0 {
		return fmt.Errorf("スナップショットが見つかりません\n  `gh pm snapshot` でスナップショットを作成してください")
	}

	// 2. 期間の計算
	from := snapshots[0].date
	to := snapshots[len(snapshots)-1].date

	// 3. トレンドを計算
	var trend *trendData
	if len(snapshots) >= 2 {
		first := snapshots[0]
		last := snapshots[len(snapshots)-1]
		wipChange := last.report.FlowMetrics.WIP - first.report.FlowMetrics.WIP
		trend = &trendData{
			WIPChange: formatChange(wipChange),
		}
	}

	// 4. スナップショットサマリー一覧
	entries := make([]snapshotEntry, 0, len(snapshots))
	for _, s := range snapshots {
		entries = append(entries, snapshotEntry{
			Date: s.date,
			Summary: snapshotSummary{
				Total:    s.report.Summary.Total,
				ByStatus: s.report.Summary.ByStatus,
				WIP:      s.report.FlowMetrics.WIP,
			},
		})
	}

	// 5. JSON 出力
	output := analyzeOutput{
		Period: periodRange{
			From: from,
			To:   to,
		},
		Mode:      flags.mode,
		Trend:     trend,
		Snapshots: entries,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON の生成に失敗しました: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// --- スナップショット読み込み ---

type loadedSnapshot struct {
	date   string
	report render.ReportJSON
}

func loadSnapshots(days int) ([]loadedSnapshot, error) {
	// .gpm-history/ 内のファイルを日付順で読み込む
	pattern := filepath.Join(snapshotDir, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("スナップショットの検索に失敗しました: %w", err)
	}

	sort.Strings(files) // ファイル名 = 日付なのでソートで時系列順に

	// 日付フィルタ
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	var snapshots []loadedSnapshot
	for _, f := range files {
		// ファイル名から日付を抽出
		base := filepath.Base(f)
		date := strings.TrimSuffix(base, ".json")
		if date < cutoff {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			continue // 読めないファイルはスキップ
		}

		var report render.ReportJSON
		if err := json.Unmarshal(data, &report); err != nil {
			continue // パースできないファイルはスキップ
		}

		snapshots = append(snapshots, loadedSnapshot{date: date, report: report})
	}

	return snapshots, nil
}

func formatChange(diff int) string {
	if diff > 0 {
		return fmt.Sprintf("+%d", diff)
	}
	return fmt.Sprintf("%d", diff)
}
