// Package analytics は GitHub Projects から取得したデータから診断シグナルを計算する。
//
// 3層アーキテクチャ:
//
//	items[]     … 生データ（各 issue の状態）
//	diagnostics … 7つの診断シグナル（統計値 + 基準値 + フラグ）
//	ai_hint     … 因果仮説 + 推奨アクション
package analytics

import (
	gh "github.com/soipon05/gh-pm/internal/github"
)

// Thresholds は診断シグナルの閾値設定。
// config.AlertConfig の値をそのまま渡す。
type Thresholds struct {
	WIPPerPerson      int // 1人あたりの WIP 上限。デフォルト 2
	AnomalyPercentile int // 異常値検出パーセンタイル。デフォルト 85
	ReviewBounce      int // 差し戻しループ閾値。デフォルト 2
}

// StatusMapper は Status 表示名（"In Progress" など）からカテゴリ名（"in_progress" など）を返す関数。
// config.CategoryOf をラップして渡すことで、analytics パッケージが config に依存しないようにする。
type StatusMapper func(statusName string) string

// Diagnostics は7つの診断シグナルの計算結果を統合する構造体。
type Diagnostics struct {
	Bottleneck   *BottleneckResult   `json:"bottleneck"`
	WIPPerPerson *WIPPerPersonResult `json:"wip_per_person"`
	FlowEff      *FlowEffResult      `json:"flow_efficiency"`
	ReviewCycles *ReviewCyclesResult  `json:"review_cycles"`
	LoadBalance  *LoadBalanceResult   `json:"load_balance"`
	Dependency   *DependencyResult    `json:"dependency"`
	Anomalies    *AnomalyResult       `json:"anomalies"`
}

// ComputeAll は全診断シグナルをまとめて計算する。
func ComputeAll(items []gh.ProjectItem, th Thresholds, mapper StatusMapper) (*Diagnostics, error) {
	return &Diagnostics{
		Bottleneck:   ComputeBottleneck(items),
		WIPPerPerson: ComputeWIPPerPerson(items, th.WIPPerPerson),
		FlowEff:      ComputeFlowEfficiency(items, mapper),
		ReviewCycles: ComputeReviewCycles(items, mapper),
		LoadBalance:  ComputeLoadBalance(items),
		Dependency:   ComputeDependency(items),
		Anomalies:    ComputeAnomalies(items, th.AnomalyPercentile),
	}, nil
}
