// Package config は .gpm.yml の読み込みと設定管理を担当する。
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config は .gpm.yml の内容をそのまま表現する構造体。
//
// Go の struct タグ（`yaml:"..."` の部分）は、YAML のキー名と
// Go のフィールド名を対応付けるための指示。
type Config struct {
	Project ProjectConfig        `yaml:"project"`
	Fields  FieldsConfig         `yaml:"fields"`
	Teams   map[string]TeamConfig `yaml:"teams"`
	Alerts  AlertConfig          `yaml:"alerts"`

	// memberTeamMap は TeamOf の O(1) ルックアップ用逆引きキャッシュ。YAML 非対象。
	memberTeamMap map[string]string `yaml:"-"`
}

// ProjectConfig は .gpm.yml の `project:` セクション。
type ProjectConfig struct {
	Owner  string `yaml:"owner"`  // GitHub Organization 名
	Number int    `yaml:"number"` // GitHub Projects 番号
}

// FieldsConfig は .gpm.yml の `fields:` セクション。
type FieldsConfig struct {
	Status StatusFieldConfig `yaml:"status"`
}

// StatusFieldConfig は Status フィールドのマッピング定義。
// GitHub Projects の Status 値を内部カテゴリ（todo/in_progress/in_review/done/blocked）に変換する。
type StatusFieldConfig struct {
	Name   string            `yaml:"name"`   // Projects 上のフィールド名
	Values StatusValuesConfig `yaml:"values"` // カテゴリ → Status 値のマッピング
}

// StatusValuesConfig は Status カテゴリと実際の値のマッピング。
type StatusValuesConfig struct {
	Todo       string `yaml:"todo"`
	InProgress string `yaml:"in_progress"`
	InReview   string `yaml:"in_review"`
	Staging    string `yaml:"staging,omitempty"`
	Done       string `yaml:"done"`
	Blocked    string `yaml:"blocked,omitempty"`
}

// TeamConfig は .gpm.yml の `teams.<name>:` セクション。
type TeamConfig struct {
	Members []string `yaml:"members"` // GitHub ID のリスト
}

// AlertConfig は .gpm.yml の `alerts:` セクション。
// diagnostics シグナルベースの閾値を定義する。
// 省略時はデフォルト値（理論的根拠に基づく推奨値）を使用する。
type AlertConfig struct {
	WIPPerPerson       int `yaml:"wip_per_person"`       // 1人あたりの WIP 上限。デフォルト 2（Little's Law）
	AnomalyPercentile  int `yaml:"anomaly_percentile"`   // 異常値検出パーセンタイル。デフォルト 85（Vacanti）
	ReviewBounce       int `yaml:"review_bounce"`         // 差し戻しループ閾値。デフォルト 2
	ZeroDoneDays       int `yaml:"zero_done_days"`        // 完了ゼロ検知期間（日）。デフォルト 7
}

// GlobalConfig は ~/.ghpm/config.yml のグローバル設定。
// 複数プロジェクトを切り替えるために使う。
type GlobalConfig struct {
	CurrentProject string         `yaml:"current_project"` // 現在アクティブなプロジェクト名
	Projects       []ProjectEntry `yaml:"projects"`        // 登録済みプロジェクトの一覧
}

// ProjectEntry はグローバル設定に登録されたプロジェクト1件分。
type ProjectEntry struct {
	Name string `yaml:"name"` // プロジェクト名（表示用）
	Path string `yaml:"path"` // .gpm.yml の絶対パス
}

// DefaultAlertConfig はデフォルトのアラート閾値を返す。
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		WIPPerPerson:      2,
		AnomalyPercentile: 85,
		ReviewBounce:      2,
		ZeroDoneDays:      7,
	}
}

// Load は指定されたパスの .gpm.yml を読み込んで Config を返す。
// ファイル読み込み → YAML パース → デフォルト適用 → バリデーション の順で処理する。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("設定ファイルを読み込めません: %s\n  `gh pm init` で設定ファイルを作成してください", path)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルの形式が不正です: %s\n  YAML の書き方を確認してください: %w", path, err)
	}

	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("設定ファイルに不備があります: %s\n  %w", path, err)
	}

	return &cfg, nil
}

// FindConfigPath は .gpm.yml のパスを探して返す。
//
// 優先順位:
//  1. 環境変数 GH_PM_CONFIG
//  2. カレントディレクトリの .gpm.yml
//  3. ~/.ghpm/config.yml の current_project が指すパス
func FindConfigPath() (string, error) {
	// 1. 環境変数
	if envPath := os.Getenv("GH_PM_CONFIG"); envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", fmt.Errorf("GH_PM_CONFIG で指定されたファイルが見つかりません: %s", envPath)
		}
		return envPath, nil
	}

	// 2. カレントディレクトリ
	const localFile = ".gpm.yml"
	if _, err := os.Stat(localFile); err == nil {
		return localFile, nil
	}

	// 3. グローバル設定
	globalPath, err := resolveFromGlobalConfig()
	if err == nil {
		return globalPath, nil
	}

	return "", fmt.Errorf("設定ファイルが見つかりません\n  `gh pm init` を実行するか、.gpm.yml を作成してください")
}

// resolveFromGlobalConfig は ~/.ghpm/config.yml を読み取り、
// current_project が指す .gpm.yml のパスを返す。
func resolveFromGlobalConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	globalFile := filepath.Join(home, ".ghpm", "config.yml")
	data, err := os.ReadFile(globalFile)
	if err != nil {
		return "", err
	}

	var gc GlobalConfig
	if err := yaml.Unmarshal(data, &gc); err != nil {
		return "", err
	}

	if gc.CurrentProject == "" {
		return "", fmt.Errorf("current_project が設定されていません")
	}

	for _, p := range gc.Projects {
		if p.Name == gc.CurrentProject {
			if _, err := os.Stat(p.Path); err != nil {
				return "", fmt.Errorf("プロジェクト %q のパスが無効です: %s", p.Name, p.Path)
			}
			return p.Path, nil
		}
	}

	return "", fmt.Errorf("プロジェクト %q が見つかりません", gc.CurrentProject)
}

// applyDefaults はゼロ値のフィールドにデフォルト値を適用する。
// YAML で省略されたフィールドは Go のゼロ値になるので、
// ここでデフォルト値を埋める。
func applyDefaults(cfg *Config) {
	defaults := DefaultAlertConfig()

	if cfg.Alerts.WIPPerPerson == 0 {
		cfg.Alerts.WIPPerPerson = defaults.WIPPerPerson
	}
	if cfg.Alerts.AnomalyPercentile == 0 {
		cfg.Alerts.AnomalyPercentile = defaults.AnomalyPercentile
	}
	if cfg.Alerts.ReviewBounce == 0 {
		cfg.Alerts.ReviewBounce = defaults.ReviewBounce
	}
	if cfg.Alerts.ZeroDoneDays == 0 {
		cfg.Alerts.ZeroDoneDays = defaults.ZeroDoneDays
	}

	if cfg.Fields.Status.Name == "" {
		cfg.Fields.Status.Name = "Status"
	}

	if cfg.Teams == nil {
		cfg.Teams = map[string]TeamConfig{}
	}
}

// Validate は Config の必須フィールドをチェックする。
// エラーメッセージには「次に何をすべきか」を含める。
func (c *Config) Validate() error {
	if c.Project.Owner == "" {
		return fmt.Errorf("project.owner が未設定です。GitHub Organization 名を設定してください")
	}
	if c.Project.Number <= 0 {
		return fmt.Errorf("project.number が不正です（1以上の整数を指定してください）")
	}
	return nil
}

// CategoryOf は Status 値からカテゴリ名（todo, in_progress, in_review, staging, done, blocked）を返す。
// 該当しない場合は空文字を返す。
func (c *Config) CategoryOf(statusName string) string {
	if statusName == "" {
		return ""
	}

	v := c.Fields.Status.Values
	switch statusName {
	case v.Todo:
		return "todo"
	case v.InProgress:
		return "in_progress"
	case v.InReview:
		return "in_review"
	case v.Staging:
		if v.Staging != "" {
			return "staging"
		}
	case v.Done:
		return "done"
	case v.Blocked:
		if v.Blocked != "" {
			return "blocked"
		}
	}
	return ""
}

// TeamOf は GitHub ID からチーム名を返す。
// 初回呼び出し時に逆引きマップを構築し、以降は O(1) で返す。
func (c *Config) TeamOf(githubID string) string {
	if c.memberTeamMap == nil {
		c.memberTeamMap = make(map[string]string)
		for teamName, team := range c.Teams {
			for _, member := range team.Members {
				c.memberTeamMap[member] = teamName
			}
		}
	}
	return c.memberTeamMap[githubID]
}
