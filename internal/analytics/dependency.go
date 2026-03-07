package analytics

import (
	gh "github.com/soipon05/gh-pm/internal/github"
)

// DependencyChain は依存ブロックの連鎖。
type DependencyChain struct {
	RootItem   int   `json:"root_item"`   // 起点アイテム番号
	ChainDepth int   `json:"chain_depth"` // 連鎖の深さ
	BlockedBy  []int `json:"blocked_by"`  // ブロックしているアイテム番号
}

// DependencyResult は signal 6: 依存ブロックの結果。
// issue 間のブロック関係チェーンで連鎖的遅延のリスクを判断できる。
type DependencyResult struct {
	Chains []DependencyChain `json:"chains"`
}

// ComputeDependency は依存ブロックチェーンを検出する。
//
// 現在の実装: StatusCategory が "blocked" のアイテムを検出する。
// issue 間のリンク情報（blocked by #XX）の解析は GitHub API の制約上、
// 将来の拡張で対応する。
func ComputeDependency(items []gh.ProjectItem) *DependencyResult {
	var chains []DependencyChain

	for _, item := range items {
		if item.StatusCategory == "blocked" {
			chains = append(chains, DependencyChain{
				RootItem:   item.Number,
				ChainDepth: 1,
				BlockedBy:  []int{},
			})
		}
	}

	return &DependencyResult{Chains: chains}
}
