// Package render はターミナルへの出力を担当する。
// GitHub API から取得したデータを整形してユーザーに見せる。
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

// padRight はターミナル表示幅を考慮して右をスペースで埋める。
// Go 標準の %-Ns は日本語などの全角文字を1幅と誤カウントするため、
// runewidth.StringWidth で実際の表示幅を計算して補正する。
func padRight(s string, width int) string {
	displayWidth := runewidth.StringWidth(s)
	if displayWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-displayWidth)
}

// TeamSummary は1チームの進捗集計結果。
type TeamSummary struct {
	Name       string
	Todo       []Item
	InProgress []Item
	InReview   []Item
	Staging    []Item
	Done       []Item
}

// Item は表示する1件のアイテム。
type Item struct {
	Number         int
	Title          string
	Assignees      []string
	Status         string
	StatusCategory string // "todo" / "in_progress" / "in_review" / "staging" / "done" / "blocked"
	ElapsedDays    int
	AlertLevel     string // "" / "warning" / "critical"
}

// PrintSummaryTable は全チームのサマリーテーブルをターミナルに出力する。
func PrintSummaryTable(teams []TeamSummary, noColor bool) {
	if noColor {
		color.NoColor = true
	}

	now := time.Now()
	fmt.Printf("=== プロジェクト進捗 (%s) ===\n\n", now.Format("2006-01-02"))

	// ヘッダー
	fmt.Printf("%s  %5s  %12s  %10s  %9s  %8s\n", padRight("チーム", 12), "Todo", "In Progress", "In Review", "Staging", "Done(7d)")
	fmt.Println("────────────────────────────────────────────────────────────")

	totalTodo, totalIP, totalIR, totalStg, totalDone := 0, 0, 0, 0, 0
	for _, t := range teams {
		fmt.Printf("%s  %5d  %12d  %10d  %9d  %8d\n",
			padRight(t.Name, 12), len(t.Todo), len(t.InProgress), len(t.InReview), len(t.Staging), len(t.Done))
		totalTodo += len(t.Todo)
		totalIP += len(t.InProgress)
		totalIR += len(t.InReview)
		totalStg += len(t.Staging)
		totalDone += len(t.Done)
	}

	fmt.Println()
	fmt.Printf("%s  %5d  %12d  %10d  %9d  %8d\n", padRight("合計:", 12), totalTodo, totalIP, totalIR, totalStg, totalDone)
}

// PrintTeamDetail は1チームの詳細をターミナルに出力する。
func PrintTeamDetail(team TeamSummary, noColor bool) {
	if noColor {
		color.NoColor = true
	}

	fmt.Printf("\n─── %s チーム詳細 ────────────────────────\n", team.Name)

	printStatusSection("In Progress", "in_progress", team.InProgress, noColor)
	printStatusSection("In Review", "in_review", team.InReview, noColor)
	printStatusSection("Staging", "staging", team.Staging, noColor)
	printStatusSection("Todo", "todo", team.Todo, noColor)
}

// printStatusSection はステータスごとのアイテムリストを出力する。
func printStatusSection(name, category string, items []Item, noColor bool) {
	if len(items) == 0 {
		return
	}

	dot := statusDot(category, noColor)
	fmt.Printf("%s %s\n", dot, name)

	for _, item := range items {
		assignee := "未アサイン"
		if len(item.Assignees) > 0 {
			assignee = strings.Join(item.Assignees, ", ")
		}

		elapsed := ""
		if item.ElapsedDays > 0 {
			elapsed = fmt.Sprintf("%dd", item.ElapsedDays)
		}

		alert := alertMarker(item.AlertLevel)
		if alert != "" && !noColor {
			if item.AlertLevel == "critical" {
				alert = color.RedString(alert)
			} else {
				alert = color.YellowString(alert)
			}
		}

		// #番号 タイトル (担当者) 経過日数 アラート
		fmt.Printf("  #%d %s (%s)", item.Number, item.Title, assignee)
		if elapsed != "" {
			fmt.Printf("  %s", elapsed)
		}
		if alert != "" {
			fmt.Printf(" %s", alert)
		}
		fmt.Println()
	}
	fmt.Println()
}

// statusDot はステータスに対応するドット記号を返す。
// --no-color 時は * を返す。
func statusDot(statusCategory string, noColor bool) string {
	if noColor {
		return "*"
	}
	switch statusCategory {
	case "in_progress":
		return color.YellowString("●")
	case "in_review":
		return color.BlueString("●")
	case "staging":
		return color.CyanString("●")
	case "todo":
		return color.WhiteString("●")
	case "done":
		return color.GreenString("●")
	default:
		return "●"
	}
}

// alertMarker はアラートレベルに基づくマーカーを返す。
func alertMarker(level string) string {
	switch level {
	case "critical":
		return "▲▲"
	case "warning":
		return "▲"
	default:
		return ""
	}
}
