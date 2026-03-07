package analytics

import (
	"sort"

	gh "github.com/soipon05/gh-pm/internal/github"
)

// ReviewCycleEntry はアイテムごとのレビューサイクル情報。
type ReviewCycleEntry struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Bounces  int    `json:"bounces"`  // ステータス逆流回数（Review → Progress）
	Comments int    `json:"comments"` // コメント数
}

// ReviewCyclesResult は signal 4: レビューサイクル数の結果。
// ステータス逆流回数でレビュープロセスの問題を判断できる。
type ReviewCyclesResult struct {
	Data []ReviewCycleEntry `json:"data"`
}

// ComputeReviewCycles はステータス逆流（差し戻し）回数を計算する。
// 遷移履歴で "in_review" → "in_progress" への逆流を bounce としてカウントする。
// mapper を使ってステータス表示名をカテゴリに変換する。
//
// Transitions が空のアイテムは計算対象外。
func ComputeReviewCycles(items []gh.ProjectItem, mapper StatusMapper) *ReviewCyclesResult {
	var entries []ReviewCycleEntry

	for _, item := range items {
		bounces := 0
		for _, t := range item.Transitions {
			fromCat := mapper(t.From)
			toCat := mapper(t.To)
			// In Review → In Progress への逆流を検出
			if fromCat == "in_review" && toCat == "in_progress" {
				bounces++
			}
		}
		if bounces > 0 {
			entries = append(entries, ReviewCycleEntry{
				Number:   item.Number,
				Title:    item.Title,
				Bounces:  bounces,
				Comments: item.CommentCount,
			})
		}
	}

	// bounce 回数の降順でソート
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Bounces > entries[j].Bounces
	})

	return &ReviewCyclesResult{Data: entries}
}
