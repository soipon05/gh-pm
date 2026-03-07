package analytics

import (
	"sort"

	gh "github.com/soipon05/gh-pm/internal/github"
)

// LoadBalanceResult は signal 5: 負荷偏りの結果。
// ジニ係数でタスク数の分布を測定し、特定の人への集中を検出する。
type LoadBalanceResult struct {
	GiniCoefficient float64        `json:"gini_coefficient"` // 0.0（完全均等）〜 1.0（完全偏り）
	Distribution    map[string]int `json:"distribution"`     // メンバー → タスク数
}

// ComputeLoadBalance は負荷偏り（ジニ係数）を計算する。
// Done のアイテムは除外する。未アサインのアイテムも除外する。
func ComputeLoadBalance(items []gh.ProjectItem) *LoadBalanceResult {
	dist := map[string]int{}
	for _, item := range items {
		if item.StatusCategory == "done" {
			continue
		}
		for _, assignee := range item.Assignees {
			dist[assignee]++
		}
	}

	if len(dist) == 0 {
		return &LoadBalanceResult{
			GiniCoefficient: 0,
			Distribution:    dist,
		}
	}

	// ジニ係数を計算
	values := make([]float64, 0, len(dist))
	for _, count := range dist {
		values = append(values, float64(count))
	}

	return &LoadBalanceResult{
		GiniCoefficient: gini(values),
		Distribution:    dist,
	}
}

// gini はジニ係数を計算する。
// 0.0 = 完全に均等な分配、1.0 = 完全に偏った分配
//
// 計算式: G = (2 * Σ(i * x_i)) / (n * Σ(x_i)) - (n+1)/n
// ここで x_i は昇順にソートされた値、i は 1-indexed の順位
func gini(values []float64) float64 {
	n := len(values)
	if n <= 1 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	var sumWeighted, sumTotal float64
	for i, v := range sorted {
		sumWeighted += float64(i+1) * v
		sumTotal += v
	}

	if sumTotal == 0 {
		return 0
	}

	return (2*sumWeighted)/(float64(n)*sumTotal) - float64(n+1)/float64(n)
}
