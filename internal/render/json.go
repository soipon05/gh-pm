package render

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/soipon05/gh-pm/internal/analytics"
	gh "github.com/soipon05/gh-pm/internal/github"
)

// ReportJSON は --format json 時の出力構造。
// requirements.md の JSON スキーマに準拠する。
type ReportJSON struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedAt   string              `json:"generated_at"`
	Project       ProjectInfo         `json:"project"`
	Team          *TeamInfo           `json:"team,omitempty"`
	FlowMetrics   FlowMetrics         `json:"flow_metrics"`
	Diagnostics   *analytics.Diagnostics `json:"diagnostics"`
	AIHint        *analytics.AIHint   `json:"ai_hint"`
	Items         []ItemJSON          `json:"items"`
	Alerts        []AlertJSON         `json:"alerts"`
	Summary       SummaryJSON         `json:"summary"`
}

// ProjectInfo はプロジェクト情報。
type ProjectInfo struct {
	Owner  string `json:"owner"`
	Number int    `json:"number"`
}

// TeamInfo はチーム情報（チーム絞り込み時のみ出力）。
type TeamInfo struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// FlowMetrics はフローメトリクス。
type FlowMetrics struct {
	WIP            int                `json:"wip"`
	TeamSize       int                `json:"team_size"`
	WIPPerPerson   float64            `json:"wip_per_person"`
	Throughput7d   int                `json:"throughput_7d"`
	Throughput30d  int                `json:"throughput_30d"`
	CycleTimeDays  *CycleTimePercent  `json:"cycle_time_days,omitempty"`
}

// CycleTimePercent はサイクルタイムのパーセンタイル。
type CycleTimePercent struct {
	P50 float64 `json:"p50"`
	P85 float64 `json:"p85"`
	P95 float64 `json:"p95"`
}

// ItemJSON は JSON 出力用のアイテム。
type ItemJSON struct {
	Number          int                   `json:"number"`
	Title           string                `json:"title"`
	URL             string                `json:"url"`
	Assignees       []string              `json:"assignees"`
	Status          string                `json:"status"`
	StatusCategory  string                `json:"status_category"`
	StatusChangedAt string                `json:"status_changed_at,omitempty"`
	ElapsedDays     int                   `json:"elapsed_days"`
	Labels          []string              `json:"labels"`
	Transitions     []TransitionJSON      `json:"transitions"`
}

// TransitionJSON は遷移履歴の JSON 表現。
type TransitionJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
	At   string `json:"at"`
}

// AlertJSON はアラートの JSON 表現。
type AlertJSON struct {
	Trigger string `json:"trigger"`
	Level   string `json:"level"`
	Detail  string `json:"detail"`
	Items   []int  `json:"items"`
	Member  string `json:"member,omitempty"`
}

// SummaryJSON はサマリー情報。
type SummaryJSON struct {
	Total        int            `json:"total"`
	ByStatus     map[string]int `json:"by_status"`
	DoneLast7d   int            `json:"done_last_7days"`
	DoneLast30d  int            `json:"done_last_30days"`
}

// BuildReportJSON は ReportJSON 構造体を構築する。
// snapshot コマンドからも使われるため、出力と分離している。
func BuildReportJSON(
	items []gh.ProjectItem,
	diag *analytics.Diagnostics,
	hint *analytics.AIHint,
	projectOwner string,
	projectNumber int,
	teamName string,
	teamMembers []string,
) ReportJSON {
	// FlowMetrics を計算
	wip := 0
	members := map[string]bool{}
	byStatus := map[string]int{}

	for _, item := range items {
		if item.StatusCategory != "done" {
			wip++
		}
		for _, a := range item.Assignees {
			members[a] = true
		}
		if item.Status != "" {
			byStatus[item.Status]++
		}
	}

	teamSize := len(members)
	var wipPerPerson float64
	if teamSize > 0 {
		wipPerPerson = float64(wip) / float64(teamSize)
	}

	// アイテムを JSON 形式に変換
	jsonItems := make([]ItemJSON, 0, len(items))
	for _, item := range items {
		transitions := make([]TransitionJSON, 0, len(item.Transitions))
		for _, t := range item.Transitions {
			transitions = append(transitions, TransitionJSON{
				From: t.From,
				To:   t.To,
				At:   t.At.Format(time.RFC3339),
			})
		}

		var changedAt string
		if !item.StatusChangedAt.IsZero() {
			changedAt = item.StatusChangedAt.Format(time.RFC3339)
		}

		jsonItems = append(jsonItems, ItemJSON{
			Number:          item.Number,
			Title:           item.Title,
			URL:             item.URL,
			Assignees:       item.Assignees,
			Status:          item.Status,
			StatusCategory:  item.StatusCategory,
			StatusChangedAt: changedAt,
			ElapsedDays:     item.ElapsedDays,
			Labels:          item.Labels,
			Transitions:     transitions,
		})
	}

	// Team 情報（絞り込み時のみ）
	var teamInfo *TeamInfo
	if teamName != "" {
		teamInfo = &TeamInfo{
			Name:    teamName,
			Members: teamMembers,
		}
	}

	return ReportJSON{
		SchemaVersion: "2.0",
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Project: ProjectInfo{
			Owner:  projectOwner,
			Number: projectNumber,
		},
		Team: teamInfo,
		FlowMetrics: FlowMetrics{
			WIP:          wip,
			TeamSize:     teamSize,
			WIPPerPerson: wipPerPerson,
		},
		Diagnostics: diag,
		AIHint:      hint,
		Items:       jsonItems,
		Alerts:      []AlertJSON{},
		Summary: SummaryJSON{
			Total:    len(items),
			ByStatus: byStatus,
		},
	}
}

// PrintJSON は diagnostics 付きの JSON を標準出力に出力する。
func PrintJSON(
	items []gh.ProjectItem,
	diag *analytics.Diagnostics,
	hint *analytics.AIHint,
	projectOwner string,
	projectNumber int,
	teamName string,
	teamMembers []string,
) error {
	report := BuildReportJSON(items, diag, hint, projectOwner, projectNumber, teamName, teamMembers)

	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON の生成に失敗しました: %w", err)
	}

	fmt.Println(string(output))
	return nil
}
