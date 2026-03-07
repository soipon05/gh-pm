package analytics

import (
	"fmt"
	"math"
	"sort"

	gh "github.com/soipon05/gh-pm/internal/github"
)

// Percentiles はステータスごとのパーセンタイル値。
// Vacanti: 「平均値は嘘をつく。ソフトウェアメトリクスは正規分布しない。」
type Percentiles struct {
	P50 float64 `json:"p50"`
	P85 float64 `json:"p85"`
	P95 float64 `json:"p95"`
}

// Outlier はパーセンタイル超過のアイテム。
type Outlier struct {
	Number  int    `json:"number"`
	Status  string `json:"status"`
	AgeDays int    `json:"age_days"`
	VsP85   string `json:"vs_p85"` // p85 との差（例: "+2 days", "at p85"）
}

// AnomalyResult は signal 7: 異常値フラグの結果。
// 指定パーセンタイル超過で即座に注目すべきアイテムを検出する。
type AnomalyResult struct {
	Percentiles map[string]Percentiles `json:"percentiles"` // ステータス → パーセンタイル
	Outliers    []Outlier              `json:"outliers"`
}

// ComputeAnomalies はパーセンタイルベースの異常値を検出する。
// ステータスごとに ElapsedDays の P50/P85/P95 を計算し、
// 指定パーセンタイル（デフォルト P85）を超えるアイテムをフラグする。
// Done のアイテムは除外する。
func ComputeAnomalies(items []gh.ProjectItem, percentileThreshold int) *AnomalyResult {
	// ステータスごとに経過日数を収集
	statusDays := map[string][]float64{}
	statusItems := map[string][]gh.ProjectItem{}

	for _, item := range items {
		if item.StatusCategory == "done" || item.Status == "" {
			continue
		}
		statusDays[item.Status] = append(statusDays[item.Status], float64(item.ElapsedDays))
		statusItems[item.Status] = append(statusItems[item.Status], item)
	}

	pctls := map[string]Percentiles{}
	var outliers []Outlier

	for status, days := range statusDays {
		sorted := make([]float64, len(days))
		copy(sorted, days)
		sort.Float64s(sorted)

		p := Percentiles{
			P50: percentile(sorted, 50),
			P85: percentile(sorted, 85),
			P95: percentile(sorted, 95),
		}
		pctls[status] = p

		// 指定パーセンタイルの閾値を計算
		threshold := percentile(sorted, float64(percentileThreshold))

		// 閾値を超えるアイテムを検出
		for _, item := range statusItems[status] {
			if float64(item.ElapsedDays) >= threshold && threshold > 0 {
				diff := item.ElapsedDays - int(math.Round(p.P85))
				var vsP85 string
				if diff > 0 {
					vsP85 = fmt.Sprintf("+%d days", diff)
				} else if diff == 0 {
					vsP85 = "at p85"
				} else {
					vsP85 = fmt.Sprintf("%d days", diff)
				}
				outliers = append(outliers, Outlier{
					Number:  item.Number,
					Status:  item.Status,
					AgeDays: item.ElapsedDays,
					VsP85:   vsP85,
				})
			}
		}
	}

	// 経過日数の降順でソート
	sort.Slice(outliers, func(i, j int) bool {
		return outliers[i].AgeDays > outliers[j].AgeDays
	})

	return &AnomalyResult{
		Percentiles: pctls,
		Outliers:    outliers,
	}
}

// percentile は昇順ソート済みのスライスから指定パーセンタイル値を線形補間で計算する。
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}

	// 0-indexed の位置を計算
	index := p / 100.0 * float64(n-1)
	lower := int(math.Floor(index))
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}

	// 線形補間
	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}
