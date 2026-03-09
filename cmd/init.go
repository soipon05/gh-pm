package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/cli/go-gh/v2/pkg/api"
	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// --- スタイル定義 ---

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("33")). // blue
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("33")).
			Padding(0, 2)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42")) // green

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")) // gray

	yamlKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")) // cyan

	yamlValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")) // white

	categoryColors = map[string]lipgloss.Style{
		"in_progress": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")), // yellow
		"in_review":   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),  // blue
		"staging":     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51")),  // teal
		"todo":        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")), // light gray
		"done":        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),  // green
		"blocked":     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")), // red
	}
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "初回セットアップウィザードを実行する",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}
	return cmd
}

// --- .gpm.yml 出力型 ---

type gpmConfig struct {
	Project gpmProject         `yaml:"project"`
	Fields  gpmFields          `yaml:"fields"`
	Teams   map[string]gpmTeam `yaml:"teams,omitempty"`
}

type gpmProject struct {
	Owner  string `yaml:"owner"`
	Number int    `yaml:"number"`
}

type gpmFields struct {
	Status gpmStatusField `yaml:"status"`
}

type gpmStatusField struct {
	Name   string            `yaml:"name"`
	Values map[string]string `yaml:"values"`
}

type gpmTeam struct {
	Members []string `yaml:"members"`
}

// --- API レスポンス型 ---

type projectFieldsResponse struct {
	Organization struct {
		ProjectV2 struct {
			Title  string `json:"title"`
			Fields struct {
				Nodes []struct {
					TypeName string `json:"__typename"`
					Name     string `json:"name"`
					Options  []struct {
						Name string `json:"name"`
					} `json:"options"`
				} `json:"nodes"`
			} `json:"fields"`
			Items struct {
				Nodes []struct {
					Content struct {
						Assignees struct {
							Nodes []struct {
								Login string `json:"login"`
							} `json:"nodes"`
						} `json:"assignees"`
					} `json:"content"`
				} `json:"nodes"`
			} `json:"items"`
		} `json:"projectV2"`
	} `json:"organization"`
}

type orgProjectsResponse struct {
	Organization struct {
		ProjectsV2 struct {
			Nodes []struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
			} `json:"nodes"`
		} `json:"projectsV2"`
	} `json:"organization"`
}

type projectInfo struct {
	Number int
	Title  string
}

// --- Status マッピングのプリセット ---

var statusPresets = map[string]string{
	"todo": "todo", "to do": "todo", "backlog": "todo",
	"in progress": "in_progress", "doing": "in_progress", "wip": "in_progress",
	"in review": "in_review", "review": "in_review", "pr review": "in_review", "code review": "in_review",
	"staging": "staging", "staging review": "staging", "stg": "staging",
	"qa": "staging", "testing": "staging", "uat": "staging",
	"done": "done", "closed": "done", "completed": "done", "released": "done",
	"blocked": "blocked",
	"未着手": "todo", "バックログ": "todo",
	"着手中": "in_progress", "作業中": "in_progress", "進行中": "in_progress",
	"レビュー中": "in_review", "レビュー待ち": "in_review", "コードレビュー": "in_review",
	"ステージング": "staging", "ステージングレビュー": "staging", "確認待ち": "staging", "QA中": "staging",
	"完了": "done", "リリース済み": "done",
	"ブロック": "blocked",
}

// runInit は初回セットアップの実際の処理。
func runInit() error {
	fmt.Println(headerStyle.Render("  gh-pm セットアップ  "))
	fmt.Println()

	// .gpm.yml が既に存在する場合は確認
	if _, err := os.Stat(".gpm.yml"); err == nil {
		var overwrite bool
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(".gpm.yml は既に存在します。上書きしますか？").
					Value(&overwrite),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if !overwrite {
			fmt.Println(dimStyle.Render("中止しました。"))
			return nil
		}
	}

	// Step 1: プロジェクト指定
	owner, number, err := selectProject()
	if err != nil {
		return err
	}

	// Step 2: API でプロジェクト情報を取得（スピナー付き）
	var statusField string
	var statusOptions []string
	var allAssignees []string

	fmt.Print(dimStyle.Render("プロジェクト情報を取得中..."))
	{
		client, apiErr := api.DefaultGraphQLClient()
		if apiErr != nil {
			fmt.Println()
			return apiErr
		}
		statusField, statusOptions, allAssignees, err = fetchProjectMetadata(client, owner, number)
	}
	fmt.Println(" " + successStyle.Render("✓"))
	if err != nil {
		return err
	}

	// Step 3: Status マッピング
	mapping, err := buildStatusMapping(statusField, statusOptions)
	if err != nil {
		return err
	}

	// Step 4: チーム定義
	teams, err := defineTeams(allAssignees)
	if err != nil {
		return err
	}

	// Step 5: プレビュー & 書き込み
	return writeConfig(owner, number, statusField, mapping, teams)
}

// selectProject はプロジェクトの指定方法を選んで owner と number を返す。
func selectProject() (owner string, number int, err error) {
	var method string
	if err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("プロジェクトの指定方法").
				Options(
					huh.NewOption("URL を貼り付け", "url"),
					huh.NewOption("Organization から選択", "select"),
				).
				Value(&method),
		),
	).WithTheme(huh.ThemeCharm()).Run(); err != nil {
		return
	}

	if method == "url" {
		var rawURL string
		if err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("プロジェクト URL").
					Description("例: https://github.com/orgs/ORG/projects/N").
					Value(&rawURL).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("URL を入力してください")
						}
						_, _, e := parseProjectURL(s)
						return e
					}),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return
		}
		owner, number, err = parseProjectURL(rawURL)
		return
	}

	// Organization をドロップダウンで選択
	fmt.Print(dimStyle.Render("Organization 一覧を取得中..."))
	orgs, orgErr := fetchUserOrgs()
	fmt.Println()
	if orgErr != nil || len(orgs) == 0 {
		// フェッチ失敗時はテキスト入力にフォールバック
		if err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Organization 名").
					Value(&owner).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("Organization 名を入力してください")
						}
						return nil
					}),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return
		}
	} else if len(orgs) == 1 {
		owner = orgs[0]
		fmt.Printf("%s %s\n\n", dimStyle.Render("Organization:"), lipgloss.NewStyle().Bold(true).Render(owner))
	} else {
		orgOpts := make([]huh.Option[string], len(orgs))
		for i, o := range orgs {
			orgOpts[i] = huh.NewOption(o, o)
		}
		if err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Organization を選択").
					Options(orgOpts...).
					Value(&owner),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return
		}
	}

	var projects []projectInfo
	fmt.Print(dimStyle.Render("プロジェクト一覧を取得中..."))
	projects, err = fetchOrgProjects(owner)
	if err != nil {
		fmt.Println()
		return
	}
	fmt.Println(" " + successStyle.Render("✓"))
	if len(projects) == 0 {
		err = fmt.Errorf("%s にアクセスできるプロジェクトが見つかりません", owner)
		return
	}

	options := make([]huh.Option[int], len(projects))
	for i, p := range projects {
		options[i] = huh.NewOption(fmt.Sprintf("#%-3d  %s", p.Number, p.Title), p.Number)
	}

	if err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("プロジェクトを選択").
				Options(options...).
				Value(&number),
		),
	).WithTheme(huh.ThemeCharm()).Run(); err != nil {
		return
	}
	return
}

// buildStatusMapping は Status 選択肢からカテゴリマッピングを構築する。
func buildStatusMapping(statusField string, statusOptions []string) (map[string]string, error) {
	fmt.Printf("\nStatus フィールド %s を検出しました:\n\n",
		lipgloss.NewStyle().Bold(true).Render("「"+statusField+"」"))

	mapping := map[string]string{}
	var unknownOptions []string

	for _, opt := range statusOptions {
		category := autoDetectCategory(opt)
		if category != "" {
			style := categoryColors[category]
			fmt.Printf("  %-22s → %s  %s\n",
				opt,
				style.Render(fmt.Sprintf("%-12s", category)),
				dimStyle.Render("✓ 自動検出"),
			)
			mapping[category] = opt
		} else {
			fmt.Printf("  %-22s → %s\n", opt, dimStyle.Render("? 未認識"))
			unknownOptions = append(unknownOptions, opt)
		}
	}
	fmt.Println()

	for _, opt := range unknownOptions {
		var category string
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(fmt.Sprintf("「%s」のカテゴリを選択", opt)).
					Options(
						huh.NewOption("todo        （未着手）", "todo"),
						huh.NewOption("in_progress （作業中）", "in_progress"),
						huh.NewOption("in_review   （レビュー中）", "in_review"),
						huh.NewOption("staging     （ステージング確認）", "staging"),
						huh.NewOption("done        （完了）", "done"),
						huh.NewOption("blocked     （ブロック）", "blocked"),
						huh.NewOption("skip        （使わない）", "skip"),
					).
					Value(&category),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return nil, err
		}
		if category != "skip" {
			mapping[category] = opt
		}
	}

	return mapping, nil
}

// defineTeams はチーム定義をインタラクティブに行う。
func defineTeams(allAssignees []string) (map[string][]string, error) {
	teams := map[string][]string{}
	if len(allAssignees) == 0 {
		return teams, nil
	}

	// 現在の認証ユーザーを取得してプリセレクトに使う
	currentUser, _ := fetchCurrentUser()

	fmt.Printf("プロジェクトのメンバー: %s 人\n\n",
		lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", len(allAssignees))))

	for {
		addTeam := len(teams) == 0 // 最初の1チームは Yes がデフォルト
		msg := "チームを定義しますか？"
		if len(teams) > 0 {
			msg = "もう1チーム追加しますか？"
		}
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().Title(msg).Value(&addTeam),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return nil, err
		}
		if !addTeam {
			break
		}

		var teamName string

		// 自分をデフォルトでプリセレクト
		members := []string{}
		for _, a := range allAssignees {
			if a == currentUser {
				members = []string{currentUser}
				break
			}
		}

		opts := make([]huh.Option[string], len(allAssignees))
		for i, a := range allAssignees {
			opts[i] = huh.NewOption(a, a)
		}

		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("チーム名").
					Description("例: backend, frontend, mobile").
					Value(&teamName).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("チーム名を入力してください")
						}
						return nil
					}),
				huh.NewMultiSelect[string]().
					Title("メンバーを選択").
					Description(fmt.Sprintf("スペースで選択、Enter で確定（あなた: %s）", dimStyle.Render(currentUser))).
					Options(opts...).
					Value(&members),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return nil, err
		}

		if len(members) > 0 {
			teams[teamName] = members
			fmt.Printf("%s %s: %s\n",
				successStyle.Render("✓"),
				lipgloss.NewStyle().Bold(true).Render(teamName),
				dimStyle.Render(strings.Join(members, ", ")),
			)
		}
	}

	return teams, nil
}

// writeConfig は .gpm.yml のプレビューを表示して書き込む。
func writeConfig(owner string, number int, statusField string, mapping map[string]string, teams map[string][]string) error {
	cfg := gpmConfig{
		Project: gpmProject{Owner: owner, Number: number},
		Fields: gpmFields{
			Status: gpmStatusField{
				Name:   statusField,
				Values: mapping,
			},
		},
	}
	if len(teams) > 0 {
		cfg.Teams = make(map[string]gpmTeam)
		for name, members := range teams {
			cfg.Teams[name] = gpmTeam{Members: members}
		}
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("YAML の生成に失敗しました: %w", err)
	}

	fmt.Printf("\n%s\n\n", lipgloss.NewStyle().Bold(true).Render("生成される .gpm.yml"))
	fmt.Println(colorizeYAML(string(data)))

	write := true // YES がデフォルト
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("この内容で .gpm.yml を生成しますか？").
				Value(&write),
		),
	).WithTheme(huh.ThemeCharm()).Run(); err != nil {
		return err
	}
	if !write {
		fmt.Println(dimStyle.Render("中止しました。"))
		return nil
	}

	if err := os.WriteFile(".gpm.yml", data, 0644); err != nil {
		return fmt.Errorf("設定ファイルの書き込みに失敗しました: %w", err)
	}

	fmt.Println()
	fmt.Println(successStyle.Render("✓ .gpm.yml を生成しました！"))

	// キャッシュ先焼き（初回 gh pm report を即時表示にする）
	if len(teams) > 0 {
		var prewarm bool
		prewarmDefault := true
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("プロジェクトデータを今すぐ取得しますか？").
					Description("次回 gh pm report が即時表示されます（約4秒）").
					Value(&prewarmDefault),
			),
		).WithTheme(huh.ThemeCharm()).Run(); err == nil {
			prewarm = prewarmDefault
		}
		if prewarm {
			fmt.Print(dimStyle.Render("データを取得中..."))
			client, apiErr := gh.NewClient()
			if apiErr == nil {
				allMembers := make([]string, 0)
				seen := map[string]bool{}
				for _, members := range teams {
					for _, m := range members {
						if !seen[m] {
							seen[m] = true
							allMembers = append(allMembers, m)
						}
					}
				}
				statusFieldName := "Status" // init で確認済みのフィールド名
				_, _, _ = client.ListTeamItems(owner, number, allMembers, statusFieldName)
				fmt.Println(" " + successStyle.Render("✓"))
			} else {
				fmt.Println(" " + dimStyle.Render("スキップ"))
			}
		}
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Render("次のステップ"))
	fmt.Println(dimStyle.Render("  gh pm report        ") + "全チームの進捗を表示")
	fmt.Println(dimStyle.Render("  gh pm report <team> ") + "チームの詳細を表示")
	fmt.Println(dimStyle.Render("  gh pm alert         ") + "アラートを確認")
	return nil
}

// colorizeYAML は YAML 文字列をシンタックスハイライトして返す。
func colorizeYAML(yamlStr string) string {
	lines := strings.Split(strings.TrimRight(yamlStr, "\n"), "\n")
	var out []string
	for _, line := range lines {
		// インデントを保持しつつキーと値を色付け
		trimmed := strings.TrimLeft(line, " ")
		indent := line[:len(line)-len(trimmed)]

		if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 {
			key := trimmed[:colonIdx]
			rest := trimmed[colonIdx+1:]
			colored := indent + yamlKeyStyle.Render(key) + ":"
			if rest != "" {
				colored += yamlValueStyle.Render(rest)
			}
			out = append(out, "  "+colored)
		} else if strings.HasPrefix(trimmed, "- ") {
			out = append(out, "  "+indent+dimStyle.Render("- ")+yamlValueStyle.Render(trimmed[2:]))
		} else {
			out = append(out, "  "+line)
		}
	}
	return strings.Join(out, "\n")
}

// --- API 取得関数 ---

func fetchOrgProjects(owner string) ([]projectInfo, error) {
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}
	query := `
query GetOrgProjects($owner: String!) {
  organization(login: $owner) {
    projectsV2(first: 30, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes { number title }
    }
  }
}`
	var resp orgProjectsResponse
	if err := client.Do(query, map[string]interface{}{"owner": owner}, &resp); err != nil {
		return nil, fmt.Errorf("プロジェクト一覧の取得に失敗しました: %w", err)
	}
	var projects []projectInfo
	for _, p := range resp.Organization.ProjectsV2.Nodes {
		projects = append(projects, projectInfo{Number: p.Number, Title: p.Title})
	}
	return projects, nil
}

func fetchProjectMetadata(client *api.GraphQLClient, owner string, number int) (statusFieldName string, statusOptions []string, assignees []string, err error) {
	query := `
query GetProjectMetadata($owner: String!, $number: Int!) {
  organization(login: $owner) {
    projectV2(number: $number) {
      fields(first: 30) {
        nodes {
          __typename
          ... on ProjectV2SingleSelectField {
            name
            options { name }
          }
        }
      }
      items(first: 100) {
        nodes {
          content {
            ... on Issue { assignees(first: 10) { nodes { login } } }
            ... on PullRequest { assignees(first: 10) { nodes { login } } }
          }
        }
      }
    }
  }
}`
	variables := map[string]interface{}{"owner": owner, "number": number}
	var resp projectFieldsResponse
	if err = client.Do(query, variables, &resp); err != nil {
		err = fmt.Errorf("プロジェクト情報の取得に失敗しました: %w", err)
		return
	}
	for _, field := range resp.Organization.ProjectV2.Fields.Nodes {
		if field.TypeName == "ProjectV2SingleSelectField" && strings.EqualFold(field.Name, "Status") {
			statusFieldName = field.Name
			for _, opt := range field.Options {
				statusOptions = append(statusOptions, opt.Name)
			}
			break
		}
	}
	if statusFieldName == "" {
		err = fmt.Errorf("Status フィールドが見つかりません")
		return
	}
	seen := map[string]bool{}
	for _, item := range resp.Organization.ProjectV2.Items.Nodes {
		for _, a := range item.Content.Assignees.Nodes {
			if a.Login != "" && !seen[a.Login] {
				seen[a.Login] = true
				assignees = append(assignees, a.Login)
			}
		}
	}
	return
}

func parseProjectURL(rawURL string) (string, int, error) {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimRight(rawURL, "/")
	parts := strings.Split(rawURL, "/")
	var org string
	var num int
	for i, part := range parts {
		if part == "orgs" && i+1 < len(parts) {
			org = parts[i+1]
		}
		if part == "projects" && i+1 < len(parts) {
			fmt.Sscanf(parts[i+1], "%d", &num)
		}
	}
	if org == "" || num == 0 {
		return "", 0, fmt.Errorf("Organization 名またはプロジェクト番号を検出できません\n  形式: https://github.com/orgs/ORG/projects/N")
	}
	return org, num, nil
}

// fetchUserOrgs は認証済みユーザーが所属する Organization の一覧を返す。
func fetchUserOrgs() ([]string, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, err
	}
	var orgs []struct {
		Login string `json:"login"`
	}
	if err := client.Get("user/orgs?per_page=100", &orgs); err != nil {
		return nil, err
	}
	names := make([]string, len(orgs))
	for i, o := range orgs {
		names[i] = o.Login
	}
	return names, nil
}

// fetchCurrentUser は認証済み GitHub ユーザーのログイン名を返す。
func fetchCurrentUser() (string, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return "", err
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := client.Get("user", &user); err != nil {
		return "", err
	}
	return user.Login, nil
}

func autoDetectCategory(statusValue string) string {
	lower := strings.ToLower(strings.TrimSpace(statusValue))
	if cat, ok := statusPresets[lower]; ok {
		return cat
	}
	return ""
}
