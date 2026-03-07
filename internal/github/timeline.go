package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// timelineQuery は issue のタイムラインイベントからステータス遷移を取得するクエリ。
//
// 制限事項:
//   - GitHub Projects V2 のステータス変更は issue タイムラインに直接記録されない（2026年3月時点）。
//   - クラシック Projects の MovedColumnsInProjectEvent は取得可能。
//   - Projects V2 の完全な遷移履歴が必要な場合は、gh pm snapshot で日次スナップショットを
//     蓄積し、その差分から計算するのが確実（Phase 7 で実装予定）。
const timelineQuery = `
query GetIssueTimeline($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    issue(number: $number) {
      timelineItems(first: $first, after: $after) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          __typename
          ... on AddedToProjectEvent {
            createdAt
          }
          ... on MovedColumnsInProjectEvent {
            createdAt
            previousProjectColumnName
            projectColumnName
          }
        }
      }
    }
  }
}
`

// --- タイムライン GraphQL レスポンス型 ---

type timelineResponse struct {
	Repository struct {
		Issue struct {
			TimelineItems struct {
				PageInfo pageInfo            `json:"pageInfo"`
				Nodes    []timelineEventNode `json:"nodes"`
			} `json:"timelineItems"`
		} `json:"issue"`
	} `json:"repository"`
}

type timelineEventNode struct {
	TypeName                  string `json:"__typename"`
	CreatedAt                 string `json:"createdAt"`
	PreviousProjectColumnName string `json:"previousProjectColumnName"`
	ProjectColumnName         string `json:"projectColumnName"`
}

// FetchStatusTransitions は指定アイテムのステータス遷移履歴を取得する。
//
// itemURL（例: "https://github.com/org/repo/issues/123"）からリポジトリ情報を解析し、
// issue のタイムラインイベントから MovedColumnsInProjectEvent を抽出して
// StatusTransition スライスとして返す。
//
// 制限事項:
//   - クラシック Projects のカラム移動イベントのみ検出可能。
//   - Projects V2 のステータス変更は検出できない。
//   - 完全な遷移履歴が必要な場合は snapshot 差分方式を使うこと。
func (c *Client) FetchStatusTransitions(itemURL string) ([]StatusTransition, error) {
	owner, repo, number, err := parseIssueURL(itemURL)
	if err != nil {
		return nil, fmt.Errorf("URL の解析に失敗しました: %w", err)
	}

	var transitions []StatusTransition
	var cursor interface{}

	for {
		variables := map[string]interface{}{
			"owner":  owner,
			"repo":   repo,
			"number": number,
			"first":  100,
			"after":  cursor,
		}

		var resp timelineResponse
		if err := c.gql.Do(timelineQuery, variables, &resp); err != nil {
			return nil, fmt.Errorf("タイムラインの取得に失敗しました (#%d): %w", number, err)
		}

		tl := resp.Repository.Issue.TimelineItems
		for _, node := range tl.Nodes {
			if node.TypeName == "MovedColumnsInProjectEvent" {
				at, _ := time.Parse(time.RFC3339, node.CreatedAt)
				transitions = append(transitions, StatusTransition{
					From: node.PreviousProjectColumnName,
					To:   node.ProjectColumnName,
					At:   at,
				})
			}
		}

		if !tl.PageInfo.HasNextPage {
			break
		}
		cursor = tl.PageInfo.EndCursor
	}

	return transitions, nil
}

// parseIssueURL は GitHub issue/PR の URL を解析して owner, repo, number を返す。
// 例: "https://github.com/example-org/my-repo/issues/123" → ("example-org", "my-repo", 123)
func parseIssueURL(rawURL string) (owner, repo string, number int, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", 0, err
	}

	// パス: /owner/repo/issues/123 or /owner/repo/pull/123
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 {
		return "", "", 0, fmt.Errorf("不正な issue/PR URL: %s", rawURL)
	}

	n, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("issue/PR 番号の解析に失敗しました: %w", err)
	}

	return parts[0], parts[1], n, nil
}
