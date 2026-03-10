package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
)

// StandupMember はスタンドアップ表示用の1メンバーの情報。
type StandupMember struct {
	GitHubID   string
	Level      string // "junior" / "mid" / "senior"
	InProgress []Item
	InReview   []Item
	RecentDone []Item // 直近24時間以内に完了したアイテム
	NextTodo   []Item // Todo のうち最初の1件
}

// PrintStandup はスタンドアップ表示をターミナルに出力する。
func PrintStandup(members []StandupMember, noColor bool) {
	if noColor {
		color.NoColor = true
	}

	now := time.Now()
	weekday := []string{"日", "月", "火", "水", "木", "金", "土"}[now.Weekday()]
	fmt.Printf("── 今日のスタンドアップ（%s・%s）", now.Format("2006-01-02"), weekday+"曜日")
	fmt.Println()
	fmt.Println()

	if len(members) == 0 {
		fmt.Println("  表示するメンバーがいません。.gpm.yml のチーム設定を確認してください。")
		return
	}

	for _, m := range members {
		printMemberSection(m)
	}
}

func printMemberSection(m StandupMember) {
	levelLabel := levelBadge(m.Level)
	header := color.New(color.Bold).Sprintf("● %s", m.GitHubID)
	fmt.Printf("%s %s\n", header, levelLabel)

	hasAnything := false

	// In Progress
	for _, item := range m.InProgress {
		hasAnything = true
		mark := inProgressMark(item, m.Level)
		hint := standupHint(item, m.Level)
		fmt.Printf("  %s #%d %s  %s  %d日%s\n",
			mark,
			item.Number,
			truncate(item.Title, 40),
			statusColor("in_progress", item.Status),
			item.ElapsedDays,
			hint,
		)
	}

	// In Review
	for _, item := range m.InReview {
		hasAnything = true
		hint := reviewHint(item)
		fmt.Printf("  %s #%d %s  %s  %d日%s\n",
			color.CyanString("👀"),
			item.Number,
			truncate(item.Title, 40),
			statusColor("in_review", item.Status),
			item.ElapsedDays,
			hint,
		)
	}

	// 直近完了
	for _, item := range m.RecentDone {
		hasAnything = true
		fmt.Printf("  %s #%d %s\n",
			color.GreenString("✅"),
			item.Number,
			truncate(item.Title, 40),
		)
	}

	// 次のタスク（In Progress/Reviewがない場合）
	if len(m.InProgress) == 0 && len(m.InReview) == 0 {
		for _, item := range m.NextTodo {
			hasAnything = true
			fmt.Printf("  %s 次のタスク: #%d %s\n",
				color.HiBlackString("📋"),
				item.Number,
				truncate(item.Title, 40),
			)
		}
	}

	if !hasAnything {
		fmt.Printf("  %s アクティブなタスクなし\n", color.HiBlackString("─"))
	}

	fmt.Println()
}

// inProgressMark はアイテムのレベルと経過日数に応じたマークを返す。
func inProgressMark(item Item, level string) string {
	// alertLevelがある場合はそちらを優先
	if item.AlertLevel == "critical" {
		return color.RedString("🔴")
	}
	if item.AlertLevel == "warning" {
		return color.YellowString("🟡")
	}

	// レベル別の滞留閾値
	threshold := staleDaysThreshold(level)
	if item.ElapsedDays >= threshold {
		if level == "junior" {
			return color.YellowString("⚠ ")
		}
		return color.YellowString("🟡")
	}
	return color.GreenString("🟢")
}

// standupHint はレベルと経過日数から声かけヒントを返す。
func standupHint(item Item, level string) string {
	threshold := staleDaysThreshold(level)
	if item.ElapsedDays < threshold {
		return ""
	}
	switch level {
	case "junior":
		return color.YellowString("  ← 詰まっていないか確認")
	case "mid":
		return color.YellowString("  ← ブロッカーあるか確認")
	case "senior":
		return color.HiBlackString("  ← 複雑タスク？スコープ確認")
	}
	return ""
}

// reviewHint はレビュー待ち日数に応じたヒントを返す。
func reviewHint(item Item) string {
	if item.ElapsedDays >= 2 {
		return color.YellowString("  ← レビュー催促")
	}
	return ""
}

// staleDaysThreshold はレベルごとの「声かけが必要な経過日数」閾値。
func staleDaysThreshold(level string) int {
	switch level {
	case "junior":
		return 2 // ジュニアは2日で声かけ
	case "mid":
		return 3
	case "senior":
		return 5 // シニアは複雑タスクが多いので余裕を持たせる
	}
	return 3
}

// levelBadge はレベル表示バッジを返す。
func levelBadge(level string) string {
	switch level {
	case "junior":
		return color.HiBlackString("(jr)")
	case "senior":
		return color.HiBlackString("(sr)")
	default:
		return ""
	}
}

// statusColor はステータスカテゴリに応じた色付き文字列を返す。
func statusColor(category, status string) string {
	switch category {
	case "in_progress":
		return color.BlueString(status)
	case "in_review":
		return color.CyanString(status)
	case "staging":
		return color.MagentaString(status)
	case "done":
		return color.GreenString(status)
	}
	return status
}

// truncate は文字列を指定の長さで切り詰める。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// levelOrder はソート用にレベルを数値化する（senior > mid > junior）。
func LevelOrder(level string) int {
	switch level {
	case "senior":
		return 0
	case "mid":
		return 1
	case "junior":
		return 2
	}
	return 1
}

// WIPWarning はWIP過多の警告メッセージを出力する。
func PrintWIPWarning(githubID string, count int, limit int) {
	fmt.Printf("  %s WIP %d件（上限%d）\n",
		color.RedString("▲▲"),
		count,
		limit,
	)
}

// PrintStandupFooter はフッターを出力する。
func PrintStandupFooter(noColor bool) {
	fmt.Printf("%s\n", strings.Repeat("─", 60))
	fmt.Printf("  詳細: gh pm report  |  アラート: gh pm alert  |  振り返り: gh pm retro\n")
}
