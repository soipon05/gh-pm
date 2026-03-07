package analytics

import (
	"testing"
	"time"

	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// テスト用ヘルパー: StatusMapper
func testMapper(s string) string {
	switch s {
	case "Todo":
		return "todo"
	case "In Progress":
		return "in_progress"
	case "In Review":
		return "in_review"
	case "Done":
		return "done"
	case "Blocked":
		return "blocked"
	}
	return ""
}

// テスト用ヘルパー: サンプルアイテム群
func sampleItems() []gh.ProjectItem {
	return []gh.ProjectItem{
		{Number: 101, Title: "認証API", Assignees: []string{"alice"}, Status: "In Progress", StatusCategory: "in_progress", ElapsedDays: 22},
		{Number: 102, Title: "商品API", Assignees: []string{"alice"}, Status: "In Progress", StatusCategory: "in_progress", ElapsedDays: 12},
		{Number: 103, Title: "決済API", Assignees: []string{"alice"}, Status: "In Progress", StatusCategory: "in_progress", ElapsedDays: 7},
		{Number: 104, Title: "カート機能", Assignees: []string{"alice"}, Status: "In Review", StatusCategory: "in_review", ElapsedDays: 17,
			CommentCount: 47,
			Transitions: []gh.StatusTransition{
				{From: "Todo", To: "In Progress", At: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)},
				{From: "In Progress", To: "In Review", At: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
				{From: "In Review", To: "In Progress", At: time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)},
				{From: "In Progress", To: "In Review", At: time.Date(2026, 2, 8, 0, 0, 0, 0, time.UTC)},
				{From: "In Review", To: "In Progress", At: time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)},
				{From: "In Progress", To: "In Review", At: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)},
			},
		},
		{Number: 201, Title: "注文履歴", Assignees: []string{"bob"}, Status: "In Progress", StatusCategory: "in_progress", ElapsedDays: 14},
		{Number: 202, Title: "ログイン画面", Assignees: []string{"bob"}, Status: "In Progress", StatusCategory: "in_progress", ElapsedDays: 10},
		{Number: 203, Title: "商品詳細", Assignees: []string{"bob"}, Status: "In Review", StatusCategory: "in_review", ElapsedDays: 20, CommentCount: 32},
		{Number: 301, Title: "CI/CD構築", Assignees: []string{"charlie"}, Status: "In Progress", StatusCategory: "in_progress", ElapsedDays: 24},
		{Number: 501, Title: "完了済み", Assignees: []string{"charlie"}, Status: "Done", StatusCategory: "done", ElapsedDays: 0},
	}
}

// ===========================================
// ComputeBottleneck テスト
// ===========================================

func TestComputeBottleneck(t *testing.T) {
	result := ComputeBottleneck(sampleItems())

	require.NotEmpty(t, result.Data)
	// Done は除外されている
	for _, entry := range result.Data {
		assert.NotEqual(t, "Done", entry.Status)
	}
	// 最初のエントリが最もスコアが高い
	assert.True(t, result.Data[0].Score >= result.Data[len(result.Data)-1].Score)
	// In Progress のアイテムが最も多い（6件）
	for _, entry := range result.Data {
		if entry.Status == "In Progress" {
			assert.Equal(t, 6, entry.Count)
			break
		}
	}
}

func TestComputeBottleneck_Empty(t *testing.T) {
	result := ComputeBottleneck([]gh.ProjectItem{})
	assert.Empty(t, result.Data)
}

// ===========================================
// ComputeWIPPerPerson テスト
// ===========================================

func TestComputeWIPPerPerson(t *testing.T) {
	result := ComputeWIPPerPerson(sampleItems(), 2)

	require.NotEmpty(t, result.Data)
	// alice は 4件 → critical
	for _, entry := range result.Data {
		if entry.Member == "alice" {
			assert.Equal(t, 4, entry.WIP)
			assert.Equal(t, "critical", entry.Flag)
		}
	}
	// charlie は 1件（done 除外）→ normal
	for _, entry := range result.Data {
		if entry.Member == "charlie" {
			assert.Equal(t, 1, entry.WIP)
			assert.Equal(t, "normal", entry.Flag)
		}
	}
	// WIP 降順でソートされている
	for i := 0; i < len(result.Data)-1; i++ {
		assert.True(t, result.Data[i].WIP >= result.Data[i+1].WIP)
	}
	// チーム平均
	assert.True(t, result.TeamAvg > 0)
}

func TestComputeWIPPerPerson_ThresholdWarning(t *testing.T) {
	items := []gh.ProjectItem{
		{Number: 1, Assignees: []string{"dev"}, Status: "In Progress", StatusCategory: "in_progress"},
		{Number: 2, Assignees: []string{"dev"}, Status: "In Review", StatusCategory: "in_review"},
	}
	result := ComputeWIPPerPerson(items, 2)
	require.Len(t, result.Data, 1)
	assert.Equal(t, "warning", result.Data[0].Flag) // WIP == threshold
}

// ===========================================
// ComputeFlowEfficiency テスト
// ===========================================

func TestComputeFlowEfficiency_WithTransitions(t *testing.T) {
	items := sampleItems()
	result := ComputeFlowEfficiency(items, testMapper)
	// item #104 のみ遷移あり → フロー効率が計算される
	assert.True(t, result.Efficiency > 0)
	assert.True(t, result.ActiveTimeDays > 0)
	assert.True(t, result.LeadTimeDays > 0)
}

func TestComputeFlowEfficiency_NoTransitions(t *testing.T) {
	items := []gh.ProjectItem{
		{Number: 1, Status: "In Progress", StatusCategory: "in_progress"},
	}
	result := ComputeFlowEfficiency(items, testMapper)
	assert.Equal(t, float64(0), result.Efficiency)
}

// ===========================================
// ComputeReviewCycles テスト
// ===========================================

func TestComputeReviewCycles(t *testing.T) {
	items := sampleItems()
	result := ComputeReviewCycles(items, testMapper)

	// item #104 は 2回の bounce（In Review → In Progress）
	require.NotEmpty(t, result.Data)
	found := false
	for _, entry := range result.Data {
		if entry.Number == 104 {
			assert.Equal(t, 2, entry.Bounces)
			assert.Equal(t, 47, entry.Comments)
			found = true
		}
	}
	assert.True(t, found, "#104 should have review bounces")
}

func TestComputeReviewCycles_NoBounces(t *testing.T) {
	items := []gh.ProjectItem{
		{Number: 1, Transitions: []gh.StatusTransition{
			{From: "Todo", To: "In Progress"},
			{From: "In Progress", To: "In Review"},
			{From: "In Review", To: "Done"},
		}},
	}
	result := ComputeReviewCycles(items, testMapper)
	assert.Empty(t, result.Data)
}

// ===========================================
// ComputeLoadBalance テスト
// ===========================================

func TestComputeLoadBalance(t *testing.T) {
	result := ComputeLoadBalance(sampleItems())

	assert.True(t, result.GiniCoefficient >= 0)
	assert.True(t, result.GiniCoefficient <= 1)
	// alice: 4, bob: 3, charlie: 1 → 偏りあり
	assert.True(t, result.GiniCoefficient > 0.1)
	assert.Equal(t, 4, result.Distribution["alice"])
	assert.Equal(t, 3, result.Distribution["bob"])
	assert.Equal(t, 1, result.Distribution["charlie"]) // done は除外
}

func TestComputeLoadBalance_EqualDistribution(t *testing.T) {
	items := []gh.ProjectItem{
		{Number: 1, Assignees: []string{"a"}, StatusCategory: "in_progress"},
		{Number: 2, Assignees: []string{"b"}, StatusCategory: "in_progress"},
		{Number: 3, Assignees: []string{"c"}, StatusCategory: "in_progress"},
	}
	result := ComputeLoadBalance(items)
	// 完全に均等 → ジニ係数 ≈ 0
	assert.InDelta(t, 0, result.GiniCoefficient, 0.01)
}

func TestComputeLoadBalance_Empty(t *testing.T) {
	result := ComputeLoadBalance([]gh.ProjectItem{})
	assert.Equal(t, float64(0), result.GiniCoefficient)
}

// ===========================================
// ComputeDependency テスト
// ===========================================

func TestComputeDependency_Blocked(t *testing.T) {
	items := []gh.ProjectItem{
		{Number: 1, StatusCategory: "blocked"},
		{Number: 2, StatusCategory: "in_progress"},
	}
	result := ComputeDependency(items)
	require.Len(t, result.Chains, 1)
	assert.Equal(t, 1, result.Chains[0].RootItem)
}

func TestComputeDependency_NoBlocked(t *testing.T) {
	result := ComputeDependency(sampleItems())
	assert.Empty(t, result.Chains)
}

// ===========================================
// ComputeAnomalies テスト
// ===========================================

func TestComputeAnomalies(t *testing.T) {
	result := ComputeAnomalies(sampleItems(), 85)

	// パーセンタイルが計算されている
	require.Contains(t, result.Percentiles, "In Progress")
	require.Contains(t, result.Percentiles, "In Review")

	// In Progress の P50 は中央値
	ipPct := result.Percentiles["In Progress"]
	assert.True(t, ipPct.P50 > 0)
	assert.True(t, ipPct.P85 >= ipPct.P50)
	assert.True(t, ipPct.P95 >= ipPct.P85)

	// Done は除外されている
	assert.NotContains(t, result.Percentiles, "Done")
}

func TestComputeAnomalies_Empty(t *testing.T) {
	result := ComputeAnomalies([]gh.ProjectItem{}, 85)
	assert.Empty(t, result.Percentiles)
	assert.Empty(t, result.Outliers)
}

// ===========================================
// percentile 関数テスト
// ===========================================

func TestPercentile(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	assert.InDelta(t, 5.5, percentile(data, 50), 0.01)
	assert.InDelta(t, 1.0, percentile(data, 0), 0.01)
	assert.InDelta(t, 10.0, percentile(data, 100), 0.01)
}

func TestPercentile_Single(t *testing.T) {
	assert.Equal(t, 42.0, percentile([]float64{42}, 50))
}

func TestPercentile_Empty(t *testing.T) {
	assert.Equal(t, 0.0, percentile([]float64{}, 50))
}

// ===========================================
// gini 関数テスト
// ===========================================

func TestGini_Equal(t *testing.T) {
	// 完全均等 → 0
	assert.InDelta(t, 0, gini([]float64{10, 10, 10}), 0.01)
}

func TestGini_Unequal(t *testing.T) {
	// 偏りあり → 正の値
	g := gini([]float64{1, 1, 1, 100})
	assert.True(t, g > 0.5)
}

func TestGini_Single(t *testing.T) {
	assert.Equal(t, 0.0, gini([]float64{5}))
}

// ===========================================
// GenerateHint テスト
// ===========================================

func TestGenerateHint_WIPOverload(t *testing.T) {
	diag := &Diagnostics{
		WIPPerPerson: &WIPPerPersonResult{
			Data:    []WIPEntry{{Member: "alice", WIP: 4, Items: []int{1, 2, 3, 4}, Flag: "critical"}},
			TeamAvg: 4.0,
		},
		Bottleneck:   &BottleneckResult{Data: []BottleneckEntry{}},
		ReviewCycles: &ReviewCyclesResult{Data: []ReviewCycleEntry{}},
		Anomalies:    &AnomalyResult{Outliers: []Outlier{}},
		LoadBalance:  &LoadBalanceResult{GiniCoefficient: 0},
	}
	th := Thresholds{WIPPerPerson: 2, ReviewBounce: 2}

	hint := GenerateHint(diag, th)
	assert.Contains(t, hint.PrioritySignals, "wip_per_person")
	assert.Contains(t, hint.RootCauseHypothesis, "WIP過多")
	assert.NotEmpty(t, hint.RecommendedActions)
}

func TestGenerateHint_NoProblems(t *testing.T) {
	diag := &Diagnostics{
		WIPPerPerson: &WIPPerPersonResult{Data: []WIPEntry{}, TeamAvg: 1.0},
		Bottleneck:   &BottleneckResult{Data: []BottleneckEntry{}},
		ReviewCycles: &ReviewCyclesResult{Data: []ReviewCycleEntry{}},
		Anomalies:    &AnomalyResult{Outliers: []Outlier{}},
		LoadBalance:  &LoadBalanceResult{GiniCoefficient: 0},
	}
	th := Thresholds{WIPPerPerson: 2, ReviewBounce: 2}

	hint := GenerateHint(diag, th)
	assert.Empty(t, hint.PrioritySignals)
	assert.Contains(t, hint.RootCauseHypothesis, "特に問題は検出されていません")
}

// ===========================================
// ComputeAll 統合テスト
// ===========================================

func TestComputeAll(t *testing.T) {
	th := Thresholds{WIPPerPerson: 2, AnomalyPercentile: 85, ReviewBounce: 2}
	diag, err := ComputeAll(sampleItems(), th, testMapper)
	require.NoError(t, err)

	assert.NotNil(t, diag.Bottleneck)
	assert.NotNil(t, diag.WIPPerPerson)
	assert.NotNil(t, diag.FlowEff)
	assert.NotNil(t, diag.ReviewCycles)
	assert.NotNil(t, diag.LoadBalance)
	assert.NotNil(t, diag.Dependency)
	assert.NotNil(t, diag.Anomalies)
}
