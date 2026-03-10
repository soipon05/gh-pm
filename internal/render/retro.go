package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/soipon05/gh-pm/internal/analytics"
)

// PrintRetro はスプリント振り返りをターミナルに出力する。
func PrintRetro(r *analytics.RetroReport, noColor bool) {
	if noColor {
		color.NoColor = true
	}

	start := r.SprintStart.Format("2006-01-02")
	end := r.SprintEnd.Format("2006-01-02")
	fmt.Printf("── スプリント振り返り（%s 〜 %s）\n", start, end)
	fmt.Println()

	// スループット
	printSection("📊 スループット")
	printThroughput(r.Throughput)
	fmt.Println()

	// サイクルタイム
	printSection("⏱  サイクルタイム")
	printCycleTime(r.CycleTime)
	fmt.Println()

	// ボトルネック
	printSection("🚦 ボトルネック")
	printBottleneck(r.Bottleneck)
	fmt.Println()

	// WIP過多
	if len(r.WIPOverloads) > 0 {
		printSection("⚠  WIP過多")
		for _, o := range r.WIPOverloads {
			levelTag := ""
			if o.Level == "junior" {
				levelTag = color.YellowString(" (jr: 詰まっている可能性)")
			}
			fmt.Printf("  %s: %d件同時（上限%d）%s\n", o.GitHubID, o.Count, o.Limit, levelTag)
		}
		fmt.Println()
	}

	// SMARTアクション
	if len(r.Actions) > 0 {
		printSection("🎯 次スプリントのアクション")
		for _, a := range r.Actions {
			fmt.Printf("  %s %s\n",
				color.New(color.Bold).Sprintf("%d.", a.Priority),
				a.What,
			)
			fmt.Printf("     担当: %s  期限: %s\n", a.Who, a.When)
			fmt.Printf("     根拠: %s\n", color.HiBlackString(a.Why))
			fmt.Println()
		}
	} else {
		fmt.Println("  アクション提案なし。良いスプリントでした！")
		fmt.Println()
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  詳細: gh pm report  |  スナップショット: gh pm snapshot\n")
}

func printSection(title string) {
	fmt.Printf("%s\n", color.New(color.Bold).Sprint(title))
}

func printThroughput(t analytics.ThroughputStats) {
	if t.PrevCount > 0 {
		arrow := trendArrow(float64(t.DoneCount), float64(t.PrevCount))
		fmt.Printf("  完了: %s件  前回: %d件  %s\n",
			color.GreenString("%d", t.DoneCount),
			t.PrevCount,
			arrow,
		)
	} else {
		fmt.Printf("  完了: %s件\n", color.GreenString("%d", t.DoneCount))
		fmt.Printf("  %s\n", color.HiBlackString("（前回比較: スナップショットなし。gh pm snapshot を定期実行してください）"))
	}
}

func printCycleTime(c analytics.CycleTimeStats) {
	if c.Median == 0 && c.P85 == 0 {
		fmt.Printf("  %s\n", color.HiBlackString("完了アイテムなし（計算不可）"))
		return
	}

	medianStr := fmt.Sprintf("%.1f日", c.Median)
	p85Str := fmt.Sprintf("%.1f日", c.P85)

	if c.PrevMedian > 0 {
		medianArrow := trendArrowInverse(c.Median, c.PrevMedian) // 小さい方が良い
		p85Arrow := trendArrowInverse(c.P85, c.PrevP85)
		fmt.Printf("  中央値: %s（前回: %.1f日）%s\n", medianStr, c.PrevMedian, medianArrow)
		fmt.Printf("  P85:   %s（前回: %.1f日）%s\n", p85Str, c.PrevP85, p85Arrow)
	} else {
		fmt.Printf("  中央値: %s\n", medianStr)
		fmt.Printf("  P85:   %s\n", p85Str)
	}

	// P85が高い場合の警告
	if c.P85 >= 10 {
		fmt.Printf("  %s\n", color.YellowString("  ↑ P85が10日超え。タスク分割を検討してください"))
	}
}

func printBottleneck(b analytics.BottleneckStats) {
	if b.StageName == "" {
		fmt.Printf("  %s\n", color.HiBlackString("アクティブなアイテムなし"))
		return
	}

	if b.IsBottleneck {
		fmt.Printf("  %s %s  平均%.1f日（%d件）← 全ステージ最長\n",
			color.YellowString("▲"),
			stageDisplayName(b.StageName),
			b.AvgDays,
			b.Count,
		)
	} else {
		fmt.Printf("  %s  平均%.1f日（%d件）\n",
			stageDisplayName(b.StageName),
			b.AvgDays,
			b.Count,
		)
	}
}

func stageDisplayName(category string) string {
	switch category {
	case "in_progress":
		return "In Progress"
	case "in_review":
		return "In Review"
	case "staging":
		return "Staging"
	}
	return category
}

// trendArrow は今回が前回より大きい場合（良い）に ↑ を返す。
func trendArrow(now, prev float64) string {
	if prev == 0 {
		return ""
	}
	pct := (now - prev) / prev * 100
	if pct > 5 {
		return color.GreenString("↑%.0f%%", pct)
	}
	if pct < -5 {
		return color.YellowString("↓%.0f%%", -pct)
	}
	return color.HiBlackString("→ 横ばい")
}

// trendArrowInverse は小さい方が良い指標用（サイクルタイムなど）。
func trendArrowInverse(now, prev float64) string {
	if prev == 0 {
		return ""
	}
	pct := (now - prev) / prev * 100
	if pct < -5 {
		return color.GreenString("↓%.0f%% 改善", -pct)
	}
	if pct > 5 {
		return color.YellowString("↑%.0f%% 悪化", pct)
	}
	return color.HiBlackString("→ 横ばい")
}

// RetroSince は「X週間前から」の表示用文字列を返す。
func RetroSince(sprintDays int) string {
	weeks := sprintDays / 7
	if weeks > 0 {
		return fmt.Sprintf("直近%d週間", weeks)
	}
	return fmt.Sprintf("直近%d日", sprintDays)
}

// PrintRetroNoData はデータ不足時のメッセージを出力する。
func PrintRetroNoData() {
	fmt.Println()
	fmt.Println("  スナップショットがありません。")
	fmt.Println()
	fmt.Println("  初回実行時は現在のプロジェクト状態でサマリーを表示します。")
	fmt.Println("  継続的な比較には定期的なスナップショット保存を推奨します:")
	fmt.Println()
	fmt.Println("    gh pm snapshot   # 手動で保存")
	fmt.Printf("    %s\n", color.HiBlackString("# 自動化例（cron / GitHub Actions）"))
	fmt.Printf("    %s\n", color.HiBlackString("# 0 9 * * 1 gh pm snapshot   # 毎週月曜9時"))
	fmt.Println()
	_ = time.Now() // time import を使うため
}
