package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/soipon05/gh-pm/internal/analytics"
	"github.com/soipon05/gh-pm/internal/config"
	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/soipon05/gh-pm/internal/render"
	"github.com/spf13/cobra"
)

// reportFlags は `gh pm report` コマンドのフラグをまとめた構造体。
type reportFlags struct {
	format  string // "table" | "json" | "csv"
	noColor bool   // カラー出力を無効化するか
	refresh bool   // キャッシュを無視して最新データを取得
}

func newReportCmd() *cobra.Command {
	flags := &reportFlags{}

	cmd := &cobra.Command{
		Use:   "report [team]",
		Short: "チーム別の進捗を表示する",
		Long: `GitHub Projects のアイテムをチーム別に集計して表示する。

チーム名を指定すると、そのチームの詳細を表示する。
指定しない場合は全チームのサマリーを表示する。`,
		Example: `  gh pm report                   # 全チームサマリー
  gh pm report backend           # backend チームの詳細
  gh pm report --format json     # AI 分析用 JSON（diagnostics 付き）
  gh pm report --format csv      # CSV 出力（スプレッドシート用）`,
		Args: cobra.MaximumNArgs(1), // 引数は 0 個または 1 個（チーム名）
		RunE: func(cmd *cobra.Command, args []string) error {
			team := ""
			if len(args) == 1 {
				team = args[0]
			}
			return runReport(appConfig, team, flags)
		},
	}

	// フラグの登録
	cmd.Flags().StringVar(&flags.format, "format", "table", "出力形式: table|json|csv")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "カラー出力を無効化する")
	cmd.Flags().BoolVar(&flags.refresh, "refresh", false, "キャッシュを無視して最新データを取得")

	return cmd
}

// runReport はレポート表示の実際の処理。
func runReport(cfg *config.Config, team string, flags *reportFlags) error {
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

	// 即時フィードバック（フリーズ感を防ぐ）
	fmt.Fprint(os.Stderr, "取得中...")

	// チーム指定あり → 対象チームメンバーの Search API 高速パス
	// チーム指定なし＋チーム設定あり → 全チームメンバーをまとめて Search API 高速パス
	// チーム設定なし → 全件スキャン（フォールバック）
	var items []gh.ProjectItem
	var cacheAge time.Duration
	if team != "" {
		if _, ok := cfg.Teams[team]; !ok {
			fmt.Fprintln(os.Stderr)
			return fmt.Errorf("チーム %q が設定ファイルに見つかりません。\n  設定されているチーム: %s", team, teamNames(cfg))
		}
		items, cacheAge, err = client.ListTeamItems(
			cfg.Project.Owner, cfg.Project.Number,
			cfg.Teams[team].Members, cfg.Fields.Status.Name,
		)
	} else if members := allUniqueMembers(cfg); len(members) > 0 {
		items, cacheAge, err = client.ListTeamItems(
			cfg.Project.Owner, cfg.Project.Number,
			members, cfg.Fields.Status.Name,
		)
	} else {
		items, err = client.ListProjectItems(cfg.Project.Owner, cfg.Project.Number, cfg.Fields.Status.Name)
	}

	// 取得完了 or エラー
	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}
	fmt.Fprintln(os.Stderr, " 完了")

	// キャッシュが古い場合は通知
	if cacheAge > 15*time.Minute {
		fmt.Fprintf(os.Stderr, "（%d分前のキャッシュ。最新化: gh pm report --refresh）\n", int(cacheAge.Minutes()))
	}

	// StatusCategory をマッピング
	for i := range items {
		items[i].StatusCategory = cfg.CategoryOf(items[i].Status)
	}

	// チーム絞り込み（高速パスでは不要だが全件スキャン時は必要）
	filtered := items
	if team != "" && len(cfg.Teams[team].Members) > 0 {
		// 高速パスは既にチームメンバーのアイテムのみ取得済みだが、
		// 複数チームへのアサインによる重複を除去する
		filtered = filterByTeam(items, cfg, team)
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
	hint := analytics.GenerateHint(diag, th)

	// 5. 出力
	switch flags.format {
	case "json":
		var teamMembers []string
		if team != "" {
			teamMembers = cfg.Teams[team].Members
		}
		return render.PrintJSON(filtered, diag, hint, cfg.Project.Owner, cfg.Project.Number, team, teamMembers)
	case "csv":
		// TODO: Phase 4 拡張
		return fmt.Errorf("CSV 出力は未実装です")
	default:
		return renderTable(cfg, items, filtered, team, diag, flags.noColor)
	}
}

// renderTable はテーブル形式でレポートを出力する。
func renderTable(cfg *config.Config, allItems, filtered []gh.ProjectItem, team string, diag *analytics.Diagnostics, noColor bool) error {
	// 異常値マップを構築（アイテム番号 → アラートレベル）
	alertLevels := buildAlertLevels(diag)

	if team == "" {
		// 全チームサマリー
		teams := groupAllTeams(allItems, cfg, alertLevels)
		render.PrintSummaryTable(teams, noColor)
	} else {
		// 特定チームの詳細
		teams := groupAllTeams(allItems, cfg, alertLevels)
		render.PrintSummaryTable(teams, noColor)

		teamSummary := buildTeamSummary(team, filtered, alertLevels)
		render.PrintTeamDetail(teamSummary, noColor)
	}

	return nil
}

// groupAllTeams は全アイテムをチームごとに集計する。
// チーム名をソートして出力順序を固定する。
func groupAllTeams(items []gh.ProjectItem, cfg *config.Config, alertLevels map[int]string) []render.TeamSummary {
	seen := map[int]bool{}
	teamItems := map[string][]gh.ProjectItem{}

	for _, item := range items {
		if seen[item.Number] {
			continue
		}
		for _, assignee := range item.Assignees {
			teamName := cfg.TeamOf(assignee)
			if teamName != "" {
				seen[item.Number] = true
				teamItems[teamName] = append(teamItems[teamName], item)
				break // 同じアイテムを複数チームに重複登録しない
			}
		}
	}

	// チーム名をソートして出力順序を固定
	names := make([]string, 0, len(cfg.Teams))
	for name := range cfg.Teams {
		names = append(names, name)
	}
	sort.Strings(names)

	teams := make([]render.TeamSummary, 0, len(names))
	for _, name := range names {
		teams = append(teams, buildTeamSummary(name, teamItems[name], alertLevels))
	}
	return teams
}

// buildTeamSummary は ProjectItem のスライスから TeamSummary を構築する。
func buildTeamSummary(name string, items []gh.ProjectItem, alertLevels map[int]string) render.TeamSummary {
	ts := render.TeamSummary{Name: name}

	for _, item := range items {
		ri := render.Item{
			Number:         item.Number,
			Title:          item.Title,
			Assignees:      item.Assignees,
			Status:         item.Status,
			StatusCategory: item.StatusCategory,
			ElapsedDays:    item.ElapsedDays,
			AlertLevel:     alertLevels[item.Number],
		}

		switch item.StatusCategory {
		case "todo":
			ts.Todo = append(ts.Todo, ri)
		case "in_progress":
			ts.InProgress = append(ts.InProgress, ri)
		case "in_review":
			ts.InReview = append(ts.InReview, ri)
		case "staging":
			ts.Staging = append(ts.Staging, ri)
		case "done":
			ts.Done = append(ts.Done, ri)
		}
	}

	return ts
}

// filterByTeam は指定チームのメンバーにアサインされたアイテムのみ抽出する。
func filterByTeam(items []gh.ProjectItem, cfg *config.Config, teamName string) []gh.ProjectItem {
	members := map[string]bool{}
	for _, m := range cfg.Teams[teamName].Members {
		members[m] = true
	}

	var filtered []gh.ProjectItem
	seen := map[int]bool{}
	for _, item := range items {
		if seen[item.Number] {
			continue
		}
		for _, assignee := range item.Assignees {
			if members[assignee] {
				filtered = append(filtered, item)
				seen[item.Number] = true
				break
			}
		}
	}
	return filtered
}

// buildAlertLevels は diagnostics の異常値から アイテム番号 → アラートレベル のマップを構築する。
func buildAlertLevels(diag *analytics.Diagnostics) map[int]string {
	levels := map[int]string{}
	if diag == nil || diag.Anomalies == nil {
		return levels
	}

	for _, o := range diag.Anomalies.Outliers {
		levels[o.Number] = "warning"
	}

	// WIP 過多の人が持つアイテムは critical
	if diag.WIPPerPerson != nil {
		for _, entry := range diag.WIPPerPerson.Data {
			if entry.Flag == "critical" {
				for _, num := range entry.Items {
					levels[num] = "critical"
				}
			}
		}
	}

	return levels
}

// allUniqueMembers は全チームのメンバーを重複なく返す。
// チーム名をソートして反復することでキャッシュキーが毎回同じになる。
func allUniqueMembers(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Teams))
	for name := range cfg.Teams {
		names = append(names, name)
	}
	sort.Strings(names)

	seen := map[string]bool{}
	var members []string
	for _, name := range names {
		for _, m := range cfg.Teams[name].Members {
			if !seen[m] {
				seen[m] = true
				members = append(members, m)
			}
		}
	}
	return members
}

// teamNames は設定されているチーム名をソートして ", " 区切りで返す。
func teamNames(cfg *config.Config) string {
	if len(cfg.Teams) == 0 {
		return "(なし)"
	}
	names := make([]string, 0, len(cfg.Teams))
	for name := range cfg.Teams {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
