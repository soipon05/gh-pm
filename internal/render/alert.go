package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/soipon05/gh-pm/internal/analytics"
)

// Alert は検出されたアラート1件。
type Alert struct {
	Trigger string `json:"trigger"` // "wip_overload", "review_bottleneck", "anomaly", "review_bounce", "zero_done"
	Level   string `json:"level"`   // "critical", "warning"
	Detail  string `json:"detail"`
	Items   []int  `json:"items"`
	Member  string `json:"member,omitempty"`
}

// GenerateAlerts は diagnostics からアラート一覧を生成する。
func GenerateAlerts(diag *analytics.Diagnostics, th analytics.Thresholds) []Alert {
	var alerts []Alert

	// WIP 過多
	if diag.WIPPerPerson != nil {
		for _, entry := range diag.WIPPerPerson.Data {
			if entry.WIP > th.WIPPerPerson {
				alerts = append(alerts, Alert{
					Trigger: "wip_overload",
					Level:   "critical",
					Detail:  fmt.Sprintf("%s: WIP %d件（閾値: %d件）", entry.Member, entry.WIP, th.WIPPerPerson),
					Items:   entry.Items,
					Member:  entry.Member,
				})
			}
		}
	}

	// レビュー滞留（In Review >= In Progress）
	if diag.Bottleneck != nil {
		var ipCount, irCount int
		for _, entry := range diag.Bottleneck.Data {
			// StatusCategory ではなく Status 名で判定する
			// ボトルネックのデータはステータス名をキーにしている
			if strings.Contains(strings.ToLower(entry.Status), "review") {
				irCount = entry.Count
			}
			if strings.Contains(strings.ToLower(entry.Status), "progress") {
				ipCount = entry.Count
			}
		}
		if irCount > 0 && irCount >= ipCount {
			alerts = append(alerts, Alert{
				Trigger: "review_bottleneck",
				Level:   "critical",
				Detail:  fmt.Sprintf("In Review %d件 >= In Progress %d件（レビューがボトルネック）", irCount, ipCount),
			})
		}
	}

	// 差し戻しループ
	if diag.ReviewCycles != nil {
		for _, entry := range diag.ReviewCycles.Data {
			if entry.Bounces >= th.ReviewBounce {
				alerts = append(alerts, Alert{
					Trigger: "review_bounce",
					Level:   "warning",
					Detail:  fmt.Sprintf("#%d %s: 差し戻し%d回 / コメント%d件", entry.Number, entry.Title, entry.Bounces, entry.Comments),
					Items:   []int{entry.Number},
				})
			}
		}
	}

	// 異常値
	if diag.Anomalies != nil {
		for _, o := range diag.Anomalies.Outliers {
			alerts = append(alerts, Alert{
				Trigger: "anomaly",
				Level:   "warning",
				Detail:  fmt.Sprintf("#%d %s %dd / %s (%s)", o.Number, "", o.AgeDays, o.Status, o.VsP85),
				Items:   []int{o.Number},
			})
		}
	}

	return alerts
}

// PrintAlertTable はアラートをターミナルに表示する。
func PrintAlertTable(alerts []Alert, noColor bool) {
	if noColor {
		color.NoColor = true
	}

	if len(alerts) == 0 {
		fmt.Println("アラートはありません")
		return
	}

	fmt.Println("=== アラート ===")
	fmt.Println()

	// トリガー種別ごとにグループ化して表示
	triggerOrder := []string{"wip_overload", "review_bottleneck", "review_bounce", "anomaly", "zero_done"}
	triggerLabel := map[string]string{
		"wip_overload":      "WIP過多",
		"review_bottleneck": "レビュー滞留",
		"review_bounce":     "差し戻しループ",
		"anomaly":           "異常値",
		"zero_done":         "完了ゼロ",
	}

	for _, trigger := range triggerOrder {
		var matching []Alert
		for _, a := range alerts {
			if a.Trigger == trigger {
				matching = append(matching, a)
			}
		}
		if len(matching) == 0 {
			continue
		}

		label := triggerLabel[trigger]
		marker := alertMarkerForLevel(matching[0].Level, noColor)
		fmt.Printf("%s %s\n", marker, label)

		for _, a := range matching {
			fmt.Printf("  %s\n", a.Detail)
		}
		fmt.Println()
	}
}

// PrintAlertJSON はアラートを JSON で出力する。
func PrintAlertJSON(alerts []Alert) error {
	output := struct {
		Alerts []Alert `json:"alerts"`
	}{Alerts: alerts}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON の生成に失敗しました: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func alertMarkerForLevel(level string, noColor bool) string {
	marker := alertMarker(level)
	if noColor || marker == "" {
		return marker
	}
	if level == "critical" {
		return color.RedString(marker)
	}
	return color.YellowString(marker)
}
