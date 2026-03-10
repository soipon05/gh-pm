package analytics

import (
	"fmt"
	"math"
	"sort"
	"time"

	gh "github.com/soipon05/gh-pm/internal/github"
)

// RetroReport はスプリント振り返りの集計結果。
type RetroReport struct {
	SprintStart  time.Time
	SprintEnd    time.Time
	Throughput   ThroughputStats
	CycleTime    CycleTimeStats
	Bottleneck   BottleneckStats
	WIPOverloads []WIPOverload
	Actions      []SmartAction
	// 前スプリントとの比較（スナップショットがある場合のみ）
	Prev *RetroReport
}

// ThroughputStats はスループット（単位期間あたりの完了件数）。
type ThroughputStats struct {
	DoneCount     int
	PrevCount     int // 前回スプリントの完了数（比較用）
	ChangePercent float64
}

// CycleTimeStats はサイクルタイム統計。
type CycleTimeStats struct {
	Median float64 // 中央値（日）
	P85    float64 // P85（日）
	PrevMedian float64
	PrevP85    float64
}

// BottleneckStats はフローのボトルネック。
type BottleneckStats struct {
	StageName   string  // 最も詰まっているステージ名
	AvgDays     float64 // そのステージの平均滞留日数
	Count       int     // そのステージの件数
	IsBottleneck bool
}

// WIPOverload はWIP過多のメンバー情報。
type WIPOverload struct {
	GitHubID string
	Count    int
	Limit    int
	Level    string
}

// SmartAction はデータドリブンで生成されたSMARTなアクション提案。
type SmartAction struct {
	Priority int    // 1=最優先
	What     string // 何をするか
	Who      string // 誰が（"チーム" / 特定メンバー）
	When     string // いつまでに
	Why      string // なぜ（データの根拠）
}

// ComputeRetro はアイテムリストからRetroReportを計算する。
func ComputeRetro(
	items []gh.ProjectItem,
	sprintDays int,
	wipLimit int,
	levelOf func(string) string,
) *RetroReport {
	now := time.Now()
	sprintStart := now.AddDate(0, 0, -sprintDays)

	report := &RetroReport{
		SprintStart: sprintStart,
		SprintEnd:   now,
	}

	// 完了アイテム（今スプリント内）
	var doneItems []gh.ProjectItem
	for _, item := range items {
		if item.StatusCategory == "done" {
			doneItems = append(doneItems, item)
		}
	}
	report.Throughput.DoneCount = len(doneItems)

	// サイクルタイム計算（完了アイテムのElapsedDaysを使う）
	report.CycleTime = computeCycleTime(doneItems)

	// ボトルネック検出（WIP中のステージ別平均滞留日数）
	report.Bottleneck = detectBottleneck(items)

	// WIP過多検出
	report.WIPOverloads = detectWIPOverloads(items, wipLimit, levelOf)

	// SMARTアクション生成
	report.Actions = generateActions(report, wipLimit)

	return report
}

func computeCycleTime(doneItems []gh.ProjectItem) CycleTimeStats {
	if len(doneItems) == 0 {
		return CycleTimeStats{}
	}

	days := make([]float64, 0, len(doneItems))
	for _, item := range doneItems {
		if item.ElapsedDays > 0 {
			days = append(days, float64(item.ElapsedDays))
		}
	}

	if len(days) == 0 {
		return CycleTimeStats{}
	}

	sort.Float64s(days)
	median := retroPercentile(days, 50)
	p85 := retroPercentile(days, 85)

	return CycleTimeStats{
		Median: math.Round(median*10) / 10,
		P85:    math.Round(p85*10) / 10,
	}
}

func detectBottleneck(items []gh.ProjectItem) BottleneckStats {
	type stageStat struct {
		totalDays int
		count     int
	}
	stages := map[string]*stageStat{}

	for _, item := range items {
		cat := item.StatusCategory
		if cat == "done" || cat == "todo" || cat == "" {
			continue
		}
		if _, ok := stages[cat]; !ok {
			stages[cat] = &stageStat{}
		}
		stages[cat].totalDays += item.ElapsedDays
		stages[cat].count++
	}

	var bottleneckStage string
	var maxAvg float64
	var maxCount int

	for stage, stat := range stages {
		if stat.count == 0 {
			continue
		}
		avg := float64(stat.totalDays) / float64(stat.count)
		if avg > maxAvg {
			maxAvg = avg
			bottleneckStage = stage
			maxCount = stat.count
		}
	}

	if bottleneckStage == "" {
		return BottleneckStats{}
	}

	return BottleneckStats{
		StageName:    bottleneckStage,
		AvgDays:      math.Round(maxAvg*10) / 10,
		Count:        maxCount,
		IsBottleneck: maxAvg >= 3.0,
	}
}

func detectWIPOverloads(items []gh.ProjectItem, limit int, levelOf func(string) string) []WIPOverload {
	// メンバーごとのWIP件数をカウント
	wipCount := map[string]int{}
	for _, item := range items {
		if item.StatusCategory == "in_progress" || item.StatusCategory == "in_review" {
			for _, assignee := range item.Assignees {
				wipCount[assignee]++
			}
		}
	}

	var overloads []WIPOverload
	for id, count := range wipCount {
		if count > limit {
			overloads = append(overloads, WIPOverload{
				GitHubID: id,
				Count:    count,
				Limit:    limit,
				Level:    levelOf(id),
			})
		}
	}

	sort.Slice(overloads, func(i, j int) bool {
		return overloads[i].Count > overloads[j].Count
	})

	return overloads
}

// generateActions はRetroReportのデータからSMARTアクションを生成する。
func generateActions(r *RetroReport, wipLimit int) []SmartAction {
	var actions []SmartAction
	priority := 1

	// WIP過多アクション
	if len(r.WIPOverloads) > 0 {
		names := make([]string, 0, len(r.WIPOverloads))
		for _, o := range r.WIPOverloads {
			names = append(names, fmt.Sprintf("%s(%d件)", o.GitHubID, o.Count))
		}
		actions = append(actions, SmartAction{
			Priority: priority,
			What:     fmt.Sprintf("WIP上限%d件ルールをボードに明記し、全員で守る", wipLimit),
			Who:      "チーム全員",
			When:     "今週中",
			Why:      fmt.Sprintf("WIP過多: %v", names),
		})
		priority++
	}

	// ボトルネックアクション
	if r.Bottleneck.IsBottleneck {
		stageName := stageDisplayName(r.Bottleneck.StageName)
		actions = append(actions, SmartAction{
			Priority: priority,
			What:     fmt.Sprintf("%s の滞留を減らす（担当固定・時間帯設定）", stageName),
			Who:      "チーム全員",
			When:     "次スプリント開始時に合意",
			Why:      fmt.Sprintf("%s 平均%.1f日（全ステージ最長）", stageName, r.Bottleneck.AvgDays),
		})
		priority++
	}

	// サイクルタイムが高い場合
	if r.CycleTime.P85 >= 7 {
		actions = append(actions, SmartAction{
			Priority: priority,
			What:     "タスクを小さく分割する（1件あたり3日以内を目安）",
			Who:      "チーム全員",
			When:     "次スプリントのリファインメントから",
			Why:      fmt.Sprintf("P85サイクルタイム %.1f日（目標: 7日以内）", r.CycleTime.P85),
		})
		priority++
	}

	// ジュニアメンバーのWIP過多は別途声かけアクション
	for _, o := range r.WIPOverloads {
		if o.Level == "junior" {
			actions = append(actions, SmartAction{
				Priority: priority,
				What:     fmt.Sprintf("%s の2時間ルールを再確認（詰まったら即共有）", o.GitHubID),
				Who:      "シニアメンバー",
				When:     "今週中",
				Why:      fmt.Sprintf("%s: WIP%d件（jr は詰まっても言い出しにくい）", o.GitHubID, o.Count),
			})
			priority++
		}
	}

	return actions
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

func retroPercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p / 100) * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
