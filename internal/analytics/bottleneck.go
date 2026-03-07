package analytics

import (
	"sort"

	gh "github.com/soipon05/gh-pm/internal/github"
)

// BottleneckEntry はステータスごとのボトルネックスコア。
// スコア = アイテム数 x 平均滞留日数
type BottleneckEntry struct {
	Status     string  `json:"status"`
	Count      int     `json:"count"`
	AvgAgeDays float64 `json:"avg_age_days"`
	Score      float64 `json:"score"`
}

// BottleneckResult は signal 1: ボトルネック検出の結果。
type BottleneckResult struct {
	Data []BottleneckEntry `json:"data"`
}

// ComputeBottleneck はステータスごとのボトルネックスコアを計算する。
// スコア = アイテム数 x 平均滞留日数。スコアが高いほどフローが詰まっている。
// Done のアイテムは除外する。
func ComputeBottleneck(items []gh.ProjectItem) *BottleneckResult {
	// ステータスごとにアイテムをグルーピング
	type group struct {
		count     int
		totalDays float64
	}
	groups := map[string]*group{}

	for _, item := range items {
		if item.StatusCategory == "done" {
			continue
		}
		if item.Status == "" {
			continue
		}
		g, ok := groups[item.Status]
		if !ok {
			g = &group{}
			groups[item.Status] = g
		}
		g.count++
		g.totalDays += float64(item.ElapsedDays)
	}

	entries := make([]BottleneckEntry, 0, len(groups))
	for status, g := range groups {
		avgDays := g.totalDays / float64(g.count)
		entries = append(entries, BottleneckEntry{
			Status:     status,
			Count:      g.count,
			AvgAgeDays: avgDays,
			Score:      float64(g.count) * avgDays,
		})
	}

	// スコア降順でソート（最もボトルネックなステータスが先頭）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})

	return &BottleneckResult{Data: entries}
}
