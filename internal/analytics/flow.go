package analytics

import (
	gh "github.com/soipon05/gh-pm/internal/github"
)

// FlowEffResult は signal 3: フロー効率の結果。
// Active time / Lead time（推定）で待ち時間の割合を判断できる。
type FlowEffResult struct {
	ActiveTimeDays float64 `json:"active_time_days"`
	LeadTimeDays   float64 `json:"lead_time_days"`
	Efficiency     float64 `json:"efficiency"` // 0.0 - 1.0
}

// ComputeFlowEfficiency はフロー効率を計算する。
// ステータス遷移履歴から Active time（作業中の時間）と Lead time（全体のリードタイム）を計算し、
// その比率をフロー効率とする。
//
// Transitions が空のアイテムは計算対象外。
// Phase 7 の snapshot 差分で遷移履歴が充実するまでは、結果が限定的になる。
func ComputeFlowEfficiency(items []gh.ProjectItem, mapper StatusMapper) *FlowEffResult {
	var totalActive, totalLead float64
	validItems := 0

	for _, item := range items {
		if len(item.Transitions) < 2 {
			continue
		}

		// リードタイム = 最初の遷移から最後の遷移までの時間
		first := item.Transitions[0].At
		last := item.Transitions[len(item.Transitions)-1].At
		leadDays := last.Sub(first).Hours() / 24
		if leadDays <= 0 {
			continue
		}

		// アクティブタイム = "in_progress" 状態だった時間の合計
		var activeDays float64
		for i := 0; i < len(item.Transitions)-1; i++ {
			t := item.Transitions[i]
			next := item.Transitions[i+1]
			category := mapper(t.To)
			if category == "in_progress" {
				activeDays += next.At.Sub(t.At).Hours() / 24
			}
		}

		totalActive += activeDays
		totalLead += leadDays
		validItems++
	}

	if validItems == 0 || totalLead == 0 {
		return &FlowEffResult{Efficiency: 0}
	}

	avgActive := totalActive / float64(validItems)
	avgLead := totalLead / float64(validItems)

	return &FlowEffResult{
		ActiveTimeDays: avgActive,
		LeadTimeDays:   avgLead,
		Efficiency:     totalActive / totalLead,
	}
}
