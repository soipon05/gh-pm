package analytics

import (
	"sort"

	gh "github.com/soipon05/gh-pm/internal/github"
)

// WIPEntry はメンバーごとの WIP 情報。
type WIPEntry struct {
	Member string `json:"member"`
	WIP    int    `json:"wip"`
	Items  []int  `json:"items"`
	Flag   string `json:"flag"` // "normal" / "warning" / "critical"
}

// WIPPerPersonResult は signal 2: WIP per person の結果。
// Little's Law: WIP増 → サイクルタイム増
type WIPPerPersonResult struct {
	Data    []WIPEntry `json:"data"`
	TeamAvg float64    `json:"team_avg"`
}

// ComputeWIPPerPerson はメンバーごとの未完了アイテム数を計算する。
// threshold を超えた人は critical、同値は warning としてフラグを立てる。
// Done のアイテムは WIP に含めない。
func ComputeWIPPerPerson(items []gh.ProjectItem, threshold int) *WIPPerPersonResult {
	// メンバーごとにアイテム番号を収集
	memberItems := map[string][]int{}
	for _, item := range items {
		if item.StatusCategory == "done" {
			continue
		}
		for _, assignee := range item.Assignees {
			memberItems[assignee] = append(memberItems[assignee], item.Number)
		}
	}

	entries := make([]WIPEntry, 0, len(memberItems))
	totalWIP := 0
	for member, itemNums := range memberItems {
		wip := len(itemNums)
		totalWIP += wip

		flag := "normal"
		if wip > threshold {
			flag = "critical"
		} else if wip == threshold {
			flag = "warning"
		}

		entries = append(entries, WIPEntry{
			Member: member,
			WIP:    wip,
			Items:  itemNums,
			Flag:   flag,
		})
	}

	// WIP 降順でソート
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].WIP > entries[j].WIP
	})

	var teamAvg float64
	if len(entries) > 0 {
		teamAvg = float64(totalWIP) / float64(len(entries))
	}

	return &WIPPerPersonResult{Data: entries, TeamAvg: teamAvg}
}
