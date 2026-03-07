package analytics

import (
	"fmt"
	"strings"
)

// AIHint は CLI がルールベースで生成する因果仮説。
// AI はこれを検証する役割に専念できる。
type AIHint struct {
	PrioritySignals     []string `json:"priority_signals"`
	RootCauseHypothesis string   `json:"root_cause_hypothesis"`
	RecommendedActions  []string `json:"recommended_actions"`
}

// GenerateHint は diagnostics の結果からルールベースで AI ヒントを生成する。
//
// ルールベース仮説の生成ロジック:
//   - 全員の WIP >= threshold → WIP過多 → コンテキストスイッチ → サイクルタイム増大
//   - ボトルネックが高スコア → ステータス滞留 → フロー停滞
//   - レビューサイクル >= threshold → 差し戻しループ → レビュー品質問題
//   - 異常値あり → 即座に注目すべきアイテム
//   - 負荷偏り（ジニ係数 > 0.3） → 特定メンバーへの過負荷
func GenerateHint(diag *Diagnostics, th Thresholds) *AIHint {
	var signals []string
	var actions []string
	var causes []string

	// WIP 過多チェック
	if diag.WIPPerPerson != nil && diag.WIPPerPerson.TeamAvg > float64(th.WIPPerPerson) {
		signals = append(signals, "wip_per_person")
		causes = append(causes, "WIP過多")
		actions = append(actions, fmt.Sprintf("WIP制限を1人%d件以下に設定", th.WIPPerPerson))

		for _, entry := range diag.WIPPerPerson.Data {
			if entry.Flag == "critical" {
				actions = append(actions, fmt.Sprintf("%s の WIP %d件を削減（現在の担当: %v）", entry.Member, entry.WIP, entry.Items))
				break
			}
		}
	}

	// ボトルネックチェック
	if diag.Bottleneck != nil && len(diag.Bottleneck.Data) > 0 {
		top := diag.Bottleneck.Data[0]
		if top.Score > 0 {
			signals = append(signals, "bottleneck")
			causes = append(causes, fmt.Sprintf("%s がボトルネック", top.Status))
			actions = append(actions, fmt.Sprintf("%s の滞留アイテム（%d件）を優先対処", top.Status, top.Count))
		}
	}

	// レビューサイクルチェック
	if diag.ReviewCycles != nil {
		for _, entry := range diag.ReviewCycles.Data {
			if entry.Bounces >= th.ReviewBounce {
				signals = append(signals, "review_cycles")
				causes = append(causes, "差し戻しループ")
				actions = append(actions, fmt.Sprintf("#%d %s（差し戻し%d回）の原因を分析", entry.Number, entry.Title, entry.Bounces))
				break
			}
		}
	}

	// 異常値チェック
	if diag.Anomalies != nil && len(diag.Anomalies.Outliers) > 0 {
		signals = append(signals, "anomalies")
		for _, o := range diag.Anomalies.Outliers {
			actions = append(actions, fmt.Sprintf("#%d（%s %dd）の状況を確認", o.Number, o.Status, o.AgeDays))
		}
	}

	// 負荷偏りチェック
	if diag.LoadBalance != nil && diag.LoadBalance.GiniCoefficient > 0.3 {
		signals = append(signals, "load_balance")
		causes = append(causes, "負荷偏り")
		actions = append(actions, "タスクの再分配を検討")
	}

	hypothesis := strings.Join(causes, " → ")
	if hypothesis == "" {
		hypothesis = "特に問題は検出されていません"
	}

	return &AIHint{
		PrioritySignals:     signals,
		RootCauseHypothesis: hypothesis,
		RecommendedActions:  actions,
	}
}
