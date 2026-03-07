package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Load 正常系 ---

func TestLoad_ValidFullConfig(t *testing.T) {
	cfg, err := Load("../../testdata/config/valid.yml")
	require.NoError(t, err)

	assert.Equal(t, "test-org", cfg.Project.Owner)
	assert.Equal(t, 42, cfg.Project.Number)
	assert.Equal(t, "Status", cfg.Fields.Status.Name)
	assert.Equal(t, "In Progress", cfg.Fields.Status.Values.InProgress)
	assert.Equal(t, "Blocked", cfg.Fields.Status.Values.Blocked)
	assert.Len(t, cfg.Teams, 2)
	assert.Equal(t, []string{"alice", "bob"}, cfg.Teams["backend"].Members)
	// alerts が明示されているのでそのまま読み込まれる
	assert.Equal(t, 3, cfg.Alerts.WIPPerPerson)
	assert.Equal(t, 90, cfg.Alerts.AnomalyPercentile)
}

func TestLoad_MinimalConfig(t *testing.T) {
	cfg, err := Load("../../testdata/config/minimal.yml")
	require.NoError(t, err)

	assert.Equal(t, "minimal-org", cfg.Project.Owner)
	assert.Equal(t, 1, cfg.Project.Number)
	// デフォルト値が適用されている
	assert.Equal(t, "Status", cfg.Fields.Status.Name)
	assert.Equal(t, 2, cfg.Alerts.WIPPerPerson)
	assert.Equal(t, 85, cfg.Alerts.AnomalyPercentile)
	assert.Equal(t, 2, cfg.Alerts.ReviewBounce)
	assert.Equal(t, 7, cfg.Alerts.ZeroDoneDays)
	// Teams は nil ではなく空 map
	assert.NotNil(t, cfg.Teams)
	assert.Empty(t, cfg.Teams)
}

func TestLoad_WithAlerts(t *testing.T) {
	cfg, err := Load("../../testdata/config/with_alerts.yml")
	require.NoError(t, err)

	assert.Equal(t, "alert-org", cfg.Project.Owner)
	assert.Equal(t, 4, cfg.Alerts.WIPPerPerson)
	assert.Equal(t, 95, cfg.Alerts.AnomalyPercentile)
	assert.Equal(t, 1, cfg.Alerts.ReviewBounce)
	assert.Equal(t, 3, cfg.Alerts.ZeroDoneDays)
	// カスタム Status 値
	assert.Equal(t, "Backlog", cfg.Fields.Status.Values.Todo)
	assert.Equal(t, "Shipped", cfg.Fields.Status.Values.Done)
}

// --- Load 異常系 ---

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("nonexistent.yml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "設定ファイルを読み込めません")
	assert.Contains(t, err.Error(), "gh pm init")
}

func TestLoad_InvalidNoOwner(t *testing.T) {
	_, err := Load("../../testdata/config/invalid_no_owner.yml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project.owner")
}

func TestLoad_InvalidNoNumber(t *testing.T) {
	_, err := Load("../../testdata/config/invalid_no_number.yml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project.number")
}

func TestLoad_InvalidYAML(t *testing.T) {
	// 不正な YAML を一時ファイルとして作成
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.yml")
	err := os.WriteFile(path, []byte(":\n  :\n  - [invalid"), 0644)
	require.NoError(t, err)

	_, err = Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "設定ファイルの形式が不正です")
}

// --- FindConfigPath ---

func TestFindConfigPath_EnvVar(t *testing.T) {
	// 一時ファイルを用意
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".gpm.yml")
	err := os.WriteFile(path, []byte("project:\n  owner: x\n  number: 1\n"), 0644)
	require.NoError(t, err)

	t.Setenv("GH_PM_CONFIG", path)

	result, err := FindConfigPath()
	require.NoError(t, err)
	assert.Equal(t, path, result)
}

func TestFindConfigPath_EnvVarNotFound(t *testing.T) {
	t.Setenv("GH_PM_CONFIG", "/nonexistent/path.yml")

	_, err := FindConfigPath()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GH_PM_CONFIG")
}

func TestFindConfigPath_LocalFile(t *testing.T) {
	// カレントディレクトリに .gpm.yml がある場合
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".gpm.yml")
	err := os.WriteFile(path, []byte("project:\n  owner: x\n  number: 1\n"), 0644)
	require.NoError(t, err)

	// 環境変数をクリア
	t.Setenv("GH_PM_CONFIG", "")

	// カレントディレクトリを変更
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	result, err := FindConfigPath()
	require.NoError(t, err)
	assert.Equal(t, ".gpm.yml", result)
}

func TestFindConfigPath_NotFound(t *testing.T) {
	tmp := t.TempDir()

	t.Setenv("GH_PM_CONFIG", "")

	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(tmp)

	_, err = FindConfigPath()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "設定ファイルが見つかりません")
}

// --- Validate ---

func TestValidate_Valid(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Owner: "org", Number: 1},
	}
	assert.NoError(t, cfg.Validate())
}

func TestValidate_MissingOwner(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Number: 1},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project.owner")
}

func TestValidate_ZeroNumber(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Owner: "org", Number: 0},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project.number")
}

func TestValidate_NegativeNumber(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Owner: "org", Number: -1},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project.number")
}

// --- applyDefaults ---

func TestApplyDefaults_AllZero(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	assert.Equal(t, 2, cfg.Alerts.WIPPerPerson)
	assert.Equal(t, 85, cfg.Alerts.AnomalyPercentile)
	assert.Equal(t, 2, cfg.Alerts.ReviewBounce)
	assert.Equal(t, 7, cfg.Alerts.ZeroDoneDays)
	assert.Equal(t, "Status", cfg.Fields.Status.Name)
	assert.NotNil(t, cfg.Teams)
}

func TestApplyDefaults_PreservesExisting(t *testing.T) {
	cfg := &Config{
		Fields: FieldsConfig{
			Status: StatusFieldConfig{Name: "MyStatus"},
		},
		Alerts: AlertConfig{
			WIPPerPerson:      5,
			AnomalyPercentile: 99,
			ReviewBounce:      10,
			ZeroDoneDays:      30,
		},
		Teams: map[string]TeamConfig{
			"team1": {Members: []string{"a"}},
		},
	}
	applyDefaults(cfg)

	assert.Equal(t, "MyStatus", cfg.Fields.Status.Name)
	assert.Equal(t, 5, cfg.Alerts.WIPPerPerson)
	assert.Equal(t, 99, cfg.Alerts.AnomalyPercentile)
	assert.Equal(t, 10, cfg.Alerts.ReviewBounce)
	assert.Equal(t, 30, cfg.Alerts.ZeroDoneDays)
	assert.Len(t, cfg.Teams, 1)
}

// --- CategoryOf ---

func TestCategoryOf_KnownValues(t *testing.T) {
	cfg := &Config{
		Fields: FieldsConfig{
			Status: StatusFieldConfig{
				Values: StatusValuesConfig{
					Todo:       "Todo",
					InProgress: "In Progress",
					InReview:   "In Review",
					Done:       "Done",
					Blocked:    "Blocked",
				},
			},
		},
	}

	assert.Equal(t, "todo", cfg.CategoryOf("Todo"))
	assert.Equal(t, "in_progress", cfg.CategoryOf("In Progress"))
	assert.Equal(t, "in_review", cfg.CategoryOf("In Review"))
	assert.Equal(t, "done", cfg.CategoryOf("Done"))
	assert.Equal(t, "blocked", cfg.CategoryOf("Blocked"))
}

func TestCategoryOf_Unknown(t *testing.T) {
	cfg := &Config{
		Fields: FieldsConfig{
			Status: StatusFieldConfig{
				Values: StatusValuesConfig{
					Todo: "Todo",
					Done: "Done",
				},
			},
		},
	}

	assert.Equal(t, "", cfg.CategoryOf("Unknown Status"))
	// Blocked が空文字の場合、"" でも空文字が返る
	assert.Equal(t, "", cfg.CategoryOf(""))
}

// --- TeamOf ---

func TestTeamOf_Found(t *testing.T) {
	cfg := &Config{
		Teams: map[string]TeamConfig{
			"backend":  {Members: []string{"alice", "bob"}},
			"frontend": {Members: []string{"charlie"}},
		},
	}

	assert.Equal(t, "backend", cfg.TeamOf("alice"))
	assert.Equal(t, "backend", cfg.TeamOf("bob"))
	assert.Equal(t, "frontend", cfg.TeamOf("charlie"))
}

func TestTeamOf_NotFound(t *testing.T) {
	cfg := &Config{
		Teams: map[string]TeamConfig{
			"backend": {Members: []string{"alice"}},
		},
	}

	assert.Equal(t, "", cfg.TeamOf("unknown-user"))
}
