package cmd

import (
	"fmt"
	"os"
	"strings"

	survey "github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "初回セットアップウィザードを実行する",
		Long: `対話形式で .gpm.yml を生成する。

フロー:
  1. プロジェクトの指定方法を選択（URL 貼り付け or Organization から選択）
  2. Status フィールドを API で自動検出し、カテゴリにマッピング
  3. チーム定義（メンバーをチェックボックスで選択）
  4. .gpm.yml を生成`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}

	return cmd
}

// --- .gpm.yml の出力型 ---
// map[string]interface{} ではなく struct を使うことで YAML キーの順序を制御する。

type gpmConfig struct {
	Project gpmProject            `yaml:"project"`
	Fields  gpmFields             `yaml:"fields"`
	Teams   map[string]gpmTeam   `yaml:"teams,omitempty"`
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

// --- init で使う API レスポンス型 ---

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
	// 英語
	"todo": "todo", "to do": "todo", "backlog": "todo",
	"in progress": "in_progress", "doing": "in_progress", "wip": "in_progress",
	"in review": "in_review", "review": "in_review", "pr review": "in_review",
	"done": "done", "closed": "done", "completed": "done",
	"blocked": "blocked",
	// 日本語
	"未着手": "todo", "バックログ": "todo",
	"着手中": "in_progress", "作業中": "in_progress", "進行中": "in_progress",
	"レビュー中": "in_review", "レビュー待ち": "in_review",
	"完了": "done",
	"ブロック": "blocked",
}

// runInit は初回セットアップの実際の処理。
func runInit() error {
	// .gpm.yml が既に存在する場合は確認
	if _, err := os.Stat(".gpm.yml"); err == nil {
		var overwrite bool
		if err := survey.AskOne(&survey.Confirm{
			Message: ".gpm.yml は既に存在します。上書きしますか？",
			Default: false,
		}, &overwrite); err != nil {
			return err
		}
		if !overwrite {
			fmt.Println("中止しました。")
			return nil
		}
	}

	fmt.Println()
	fmt.Println("=== gh-pm セットアップ ===")
	fmt.Println()

	// Step 1: プロジェクト指定
	owner, number, err := selectProject()
	if err != nil {
		return err
	}

	// Step 2: プロジェクト情報を API で取得
	fmt.Println("\nプロジェクト情報を取得中...")
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return fmt.Errorf("GitHub API クライアントの作成に失敗しました: %w", err)
	}

	statusField, statusOptions, allAssignees, err := fetchProjectMetadata(client, owner, number)
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
	if err = survey.AskOne(&survey.Select{
		Message: "プロジェクトの指定方法:",
		Options: []string{
			"URL を貼り付け",
			"Organization から選択",
		},
	}, &method); err != nil {
		return
	}

	if method == "URL を貼り付け" {
		var rawURL string
		if err = survey.AskOne(&survey.Input{
			Message: "プロジェクト URL:",
			Help:    "例: https://github.com/orgs/ORG/projects/N",
		}, &rawURL, survey.WithValidator(survey.Required)); err != nil {
			return
		}
		owner, number, err = parseProjectURL(rawURL)
		if err != nil {
			err = fmt.Errorf("URL の解析に失敗しました: %w\n  形式: https://github.com/orgs/ORG/projects/N", err)
		}
		return
	}

	// Organization から選択
	if err = survey.AskOne(&survey.Input{
		Message: "Organization 名:",
	}, &owner, survey.WithValidator(survey.Required)); err != nil {
		return
	}

	fmt.Printf("\nプロジェクト一覧を取得中...\n")
	projects, fetchErr := fetchOrgProjects(owner)
	if fetchErr != nil {
		err = fetchErr
		return
	}
	if len(projects) == 0 {
		err = fmt.Errorf("%s にアクセスできるプロジェクトが見つかりません", owner)
		return
	}

	options := make([]string, len(projects))
	for i, p := range projects {
		options[i] = fmt.Sprintf("#%d  %s", p.Number, p.Title)
	}

	var selected string
	if err = survey.AskOne(&survey.Select{
		Message: "プロジェクトを選択:",
		Options: options,
	}, &selected); err != nil {
		return
	}

	for _, p := range projects {
		if selected == fmt.Sprintf("#%d  %s", p.Number, p.Title) {
			number = p.Number
			return
		}
	}
	err = fmt.Errorf("選択されたプロジェクトが見つかりません")
	return
}

// buildStatusMapping は Status 選択肢からカテゴリマッピングを構築する。
func buildStatusMapping(statusField string, statusOptions []string) (map[string]string, error) {
	fmt.Printf("\nStatus フィールド「%s」の値を検出しました:\n\n", statusField)

	mapping := map[string]string{} // category → status_value
	var unknownOptions []string

	for _, opt := range statusOptions {
		category := autoDetectCategory(opt)
		if category != "" {
			fmt.Printf("  %-20s → %-12s ✓ 自動検出\n", opt, category)
			mapping[category] = opt
		} else {
			fmt.Printf("  %-20s → ?\n", opt)
			unknownOptions = append(unknownOptions, opt)
		}
	}

	// 未認識の値を手動でマッピング
	if len(unknownOptions) > 0 {
		fmt.Println("\n以下の値のカテゴリを選択してください:")
		for _, opt := range unknownOptions {
			var category string
			if err := survey.AskOne(&survey.Select{
				Message: fmt.Sprintf("「%s」のカテゴリ:", opt),
				Options: []string{"todo", "in_progress", "in_review", "done", "blocked", "skip（使わない）"},
			}, &category); err != nil {
				return nil, err
			}
			if !strings.HasPrefix(category, "skip") {
				mapping[category] = opt
			}
		}
	}

	fmt.Println()
	var confirm bool
	if err := survey.AskOne(&survey.Confirm{
		Message: "このマッピングでよいですか？",
		Default: true,
	}, &confirm); err != nil {
		return nil, err
	}
	if !confirm {
		fmt.Println("  .gpm.yml 生成後に手動で編集してください。")
	}

	return mapping, nil
}

// defineTeams はチーム定義をインタラクティブに行う。
func defineTeams(allAssignees []string) (map[string][]string, error) {
	teams := map[string][]string{}

	if len(allAssignees) == 0 {
		return teams, nil
	}

	fmt.Printf("\nプロジェクトのメンバー: %d 人\n", len(allAssignees))

	for {
		var addTeam bool
		prompt := "チームを定義しますか？"
		if len(teams) > 0 {
			prompt = "もう1チーム追加しますか？"
		}
		if err := survey.AskOne(&survey.Confirm{
			Message: prompt,
			Default: len(teams) == 0,
		}, &addTeam); err != nil {
			return nil, err
		}
		if !addTeam {
			break
		}

		var teamName string
		if err := survey.AskOne(&survey.Input{
			Message: "チーム名:",
			Help:    "例: backend, frontend, mobile",
		}, &teamName, survey.WithValidator(survey.Required)); err != nil {
			return nil, err
		}

		var members []string
		if err := survey.AskOne(&survey.MultiSelect{
			Message: fmt.Sprintf("%s のメンバーを選択してください:", teamName),
			Options: allAssignees,
		}, &members); err != nil {
			return nil, err
		}

		if len(members) > 0 {
			teams[teamName] = members
			fmt.Printf("  ✓ %s: %s\n", teamName, strings.Join(members, ", "))
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

	fmt.Printf("\n生成される .gpm.yml:\n\n")
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()

	var write bool
	if err := survey.AskOne(&survey.Confirm{
		Message: "この内容で .gpm.yml を生成しますか？",
		Default: true,
	}, &write); err != nil {
		return err
	}
	if !write {
		fmt.Println("中止しました。")
		return nil
	}

	if err := os.WriteFile(".gpm.yml", data, 0644); err != nil {
		return fmt.Errorf("設定ファイルの書き込みに失敗しました: %w", err)
	}

	fmt.Println("\n✓ .gpm.yml を生成しました！")
	fmt.Println()
	fmt.Println("次のステップ:")
	fmt.Println("  gh pm report        # 全チームの進捗を表示")
	fmt.Println("  gh pm report <team> # チームの詳細を表示")
	fmt.Println("  gh pm alert         # アラートを確認")
	return nil
}

// fetchOrgProjects は Organization のプロジェクト一覧を取得する。
func fetchOrgProjects(owner string) ([]projectInfo, error) {
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}

	query := `
query GetOrgProjects($owner: String!) {
  organization(login: $owner) {
    projectsV2(first: 30, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes {
        number
        title
      }
    }
  }
}`

	var resp orgProjectsResponse
	if err := client.Do(query, map[string]interface{}{"owner": owner}, &resp); err != nil {
		return nil, fmt.Errorf("プロジェクト一覧の取得に失敗しました: %w\n  Organization 名と権限を確認してください", err)
	}

	var projects []projectInfo
	for _, p := range resp.Organization.ProjectsV2.Nodes {
		projects = append(projects, projectInfo{Number: p.Number, Title: p.Title})
	}
	return projects, nil
}

// parseProjectURL は GitHub Projects の URL を解析して owner と number を返す。
func parseProjectURL(rawURL string) (string, int, error) {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimRight(rawURL, "/")

	parts := strings.Split(rawURL, "/")
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("不正な URL")
	}

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
		return "", 0, fmt.Errorf("Organization 名またはプロジェクト番号を検出できません")
	}

	return org, num, nil
}

// fetchProjectMetadata はプロジェクトの Status フィールド情報とアサイニーを取得する。
func fetchProjectMetadata(client *api.GraphQLClient, owner string, number int) (statusFieldName string, statusOptions []string, assignees []string, err error) {
	query := `
query GetProjectMetadata($owner: String!, $number: Int!) {
  organization(login: $owner) {
    projectV2(number: $number) {
      title
      fields(first: 30) {
        nodes {
          __typename
          ... on ProjectV2SingleSelectField {
            name
            options {
              name
            }
          }
        }
      }
      items(first: 100) {
        nodes {
          content {
            ... on Issue {
              assignees(first: 10) { nodes { login } }
            }
            ... on PullRequest {
              assignees(first: 10) { nodes { login } }
            }
          }
        }
      }
    }
  }
}`

	variables := map[string]interface{}{
		"owner":  owner,
		"number": number,
	}

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
		err = fmt.Errorf("Status フィールドが見つかりません\n  GitHub Projects の設定画面で Status フィールドを確認してください")
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

// autoDetectCategory は Status 値からカテゴリを自動推定する。
func autoDetectCategory(statusValue string) string {
	lower := strings.ToLower(strings.TrimSpace(statusValue))
	if cat, ok := statusPresets[lower]; ok {
		return cat
	}
	return ""
}
