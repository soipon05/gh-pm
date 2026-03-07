package cmd

import (
	"fmt"
	"os"
	"strings"

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
  1. プロジェクトの指定方法を選択（URL 貼り付け or インタラクティブ選択）
  2. Status フィールドを API で自動検出し、カテゴリにマッピング
     (todo / in_progress / in_review / done / blocked)
  3. チーム定義（チーム名 + メンバー選択をループ）
  4. .gpm.yml を生成`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}

	return cmd
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
	// .gpm.yml が既に存在する場合は警告
	if _, err := os.Stat(".gpm.yml"); err == nil {
		fmt.Print(".gpm.yml は既に存在します。上書きしますか？ (y/N): ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" {
			fmt.Println("中止しました")
			return nil
		}
	}

	// 1. プロジェクトの指定
	fmt.Println("=== gh-pm セットアップ ===")
	fmt.Println()
	fmt.Print("プロジェクト URL を入力してください（例: https://github.com/orgs/ORG/projects/N）: ")
	var projectURL string
	fmt.Scanln(&projectURL)

	owner, number, err := parseProjectURL(projectURL)
	if err != nil {
		return fmt.Errorf("URL の解析に失敗しました: %w\n  形式: https://github.com/orgs/ORG/projects/N", err)
	}

	fmt.Printf("\nプロジェクト: %s #%d\n", owner, number)

	// 2. Status フィールドを API で検出
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return fmt.Errorf("GitHub API クライアントの作成に失敗しました: %w", err)
	}

	statusField, statusOptions, assignees, err := fetchProjectMetadata(client, owner, number)
	if err != nil {
		return err
	}

	// 3. Status マッピング
	fmt.Printf("\nStatus フィールド「%s」の値を検出しました:\n", statusField)
	mapping := map[string]string{} // category → status_value
	for _, opt := range statusOptions {
		category := autoDetectCategory(opt)
		if category != "" {
			fmt.Printf("  %-20s → %s ✓ 自動検出\n", opt, category)
			mapping[category] = opt
		} else {
			fmt.Printf("  %-20s → ? (skip)\n", opt)
		}
	}

	fmt.Print("\nこのマッピングでよいですか？ (Y/n): ")
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) == "n" {
		fmt.Println("手動マッピングは未実装です。.gpm.yml を手動で編集してください。")
	}

	// 4. チーム定義
	teams := map[string][]string{} // team_name → []members
	if len(assignees) > 0 {
		fmt.Printf("\nプロジェクトのメンバー: %s\n", strings.Join(assignees, ", "))
		for {
			fmt.Print("\nチーム名を入力してください（空で終了）: ")
			var teamName string
			fmt.Scanln(&teamName)
			if teamName == "" {
				break
			}
			fmt.Printf("  %s のメンバーをカンマ区切りで入力: ", teamName)
			var membersInput string
			fmt.Scanln(&membersInput)
			members := strings.Split(membersInput, ",")
			for i := range members {
				members[i] = strings.TrimSpace(members[i])
			}
			teams[teamName] = members
			fmt.Printf("  %s: %v\n", teamName, members)
		}
	}

	// 5. .gpm.yml を生成
	cfg := buildConfigYAML(owner, number, statusField, mapping, teams)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("YAML の生成に失敗しました: %w", err)
	}

	if err := os.WriteFile(".gpm.yml", data, 0644); err != nil {
		return fmt.Errorf("設定ファイルの書き込みに失敗しました: %w", err)
	}

	fmt.Println("\n.gpm.yml を生成しました")
	return nil
}

// parseProjectURL は GitHub Projects の URL を解析して owner と number を返す。
func parseProjectURL(rawURL string) (string, int, error) {
	// https://github.com/orgs/ORG/projects/N
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
              assignees(first: 10) {
                nodes { login }
              }
            }
            ... on PullRequest {
              assignees(first: 10) {
                nodes { login }
              }
            }
          }
        }
      }
    }
  }
}
`
	variables := map[string]interface{}{
		"owner":  owner,
		"number": number,
	}

	var resp projectFieldsResponse
	if err := client.Do(query, variables, &resp); err != nil {
		return "", nil, nil, fmt.Errorf("プロジェクト情報の取得に失敗しました: %w", err)
	}

	// Status フィールドを検出
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
		return "", nil, nil, fmt.Errorf("Status フィールドが見つかりません\n  GitHub Projects の設定画面で Status フィールドを確認してください")
	}

	// アサイニーを収集（重複除去）
	seen := map[string]bool{}
	for _, item := range resp.Organization.ProjectV2.Items.Nodes {
		for _, a := range item.Content.Assignees.Nodes {
			if a.Login != "" && !seen[a.Login] {
				seen[a.Login] = true
				assignees = append(assignees, a.Login)
			}
		}
	}

	return statusFieldName, statusOptions, assignees, nil
}

// autoDetectCategory は Status 値からカテゴリを自動推定する。
func autoDetectCategory(statusValue string) string {
	lower := strings.ToLower(strings.TrimSpace(statusValue))
	if cat, ok := statusPresets[lower]; ok {
		return cat
	}
	return ""
}

// buildConfigYAML は .gpm.yml 用の構造体を構築する。
func buildConfigYAML(owner string, number int, statusFieldName string, mapping map[string]string, teams map[string][]string) map[string]interface{} {
	statusValues := map[string]string{}
	for cat, val := range mapping {
		statusValues[cat] = val
	}

	cfg := map[string]interface{}{
		"project": map[string]interface{}{
			"owner":  owner,
			"number": number,
		},
		"fields": map[string]interface{}{
			"status": map[string]interface{}{
				"name":   statusFieldName,
				"values": statusValues,
			},
		},
	}

	if len(teams) > 0 {
		teamsMap := map[string]interface{}{}
		for name, members := range teams {
			teamsMap[name] = map[string]interface{}{
				"members": members,
			}
		}
		cfg["teams"] = teamsMap
	}

	return cfg
}
