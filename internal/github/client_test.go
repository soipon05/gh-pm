package github

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGQL はテスト用の GraphQL クライアントモック。
// Do が呼ばれるたびに responses から順番にレスポンスを返す。
type mockGQL struct {
	responses []string                 // JSON レスポンス（先頭から順に消費される）
	calls     int                      // Do が呼ばれた回数
	lastVars  map[string]interface{}   // 最後に渡された variables
}

func (m *mockGQL) Do(query string, variables map[string]interface{}, response interface{}) error {
	if m.calls >= len(m.responses) {
		return fmt.Errorf("unexpected Do call #%d (only %d responses prepared)", m.calls, len(m.responses))
	}
	m.lastVars = variables
	data := m.responses[m.calls]
	m.calls++
	return json.Unmarshal([]byte(data), response)
}

// ===========================================
// ListProjectItems テスト
// ===========================================

func TestListProjectItems_SinglePage(t *testing.T) {
	mock := &mockGQL{
		responses: []string{singlePageResponse},
	}

	client := newTestClient(mock)
	items, err := client.ListProjectItems("example-org", 1, "Status")
	require.NoError(t, err)
	require.Len(t, items, 2)

	// 1件目
	assert.Equal(t, 101, items[0].Number)
	assert.Equal(t, "ユーザー認証API実装", items[0].Title)
	assert.Equal(t, "https://github.com/example-org/repo/issues/101", items[0].URL)
	assert.Equal(t, "In Progress", items[0].Status)
	assert.Equal(t, []string{"alice"}, items[0].Assignees)
	assert.Equal(t, []string{"backend", "auth"}, items[0].Labels)
	assert.Equal(t, 5, items[0].CommentCount)
	assert.False(t, items[0].StatusChangedAt.IsZero())
	assert.True(t, items[0].ElapsedDays >= 0)

	// 2件目
	assert.Equal(t, 102, items[1].Number)
	assert.Equal(t, "In Review", items[1].Status)
	assert.Equal(t, []string{"bob", "alice"}, items[1].Assignees)
	assert.Equal(t, 12, items[1].CommentCount)

	// StatusCategory は API 層では設定されない（呼び出し元の責務）
	assert.Empty(t, items[0].StatusCategory)
	assert.Empty(t, items[1].StatusCategory)

	// API が1回だけ呼ばれた
	assert.Equal(t, 1, mock.calls)
}

func TestListProjectItems_Pagination(t *testing.T) {
	mock := &mockGQL{
		responses: []string{
			paginationPage1,
			paginationPage2,
			paginationPage3,
		},
	}

	client := newTestClient(mock)
	items, err := client.ListProjectItems("org", 1, "Status")
	require.NoError(t, err)
	require.Len(t, items, 3)

	assert.Equal(t, 1, items[0].Number)
	assert.Equal(t, "Todo", items[0].Status)
	assert.Equal(t, 2, items[1].Number)
	assert.Equal(t, "Done", items[1].Status)
	assert.Equal(t, 3, items[2].Number)
	assert.Equal(t, "In Progress", items[2].Status)

	// 3回 API が呼ばれた
	assert.Equal(t, 3, mock.calls)
}

func TestListProjectItems_EmptyProject(t *testing.T) {
	mock := &mockGQL{
		responses: []string{emptyProjectResponse},
	}

	client := newTestClient(mock)
	items, err := client.ListProjectItems("org", 1, "Status")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestListProjectItems_DraftIssueSkipped(t *testing.T) {
	mock := &mockGQL{
		responses: []string{draftIssueResponse},
	}

	client := newTestClient(mock)
	items, err := client.ListProjectItems("org", 1, "Status")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 42, items[0].Number)
	assert.Equal(t, "Real Issue", items[0].Title)
}

func TestListProjectItems_CustomStatusField(t *testing.T) {
	mock := &mockGQL{
		responses: []string{customFieldResponse},
	}

	client := newTestClient(mock)
	// "進捗" という名前の Status フィールドを指定
	items, err := client.ListProjectItems("org", 1, "進捗")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "作業中", items[0].Status)
}

func TestListProjectItems_NoStatusField(t *testing.T) {
	mock := &mockGQL{
		responses: []string{noStatusFieldResponse},
	}

	client := newTestClient(mock)
	items, err := client.ListProjectItems("org", 1, "Status")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Empty(t, items[0].Status)
}

func TestListProjectItems_APIError(t *testing.T) {
	mock := &mockGQL{
		responses: []string{}, // レスポンスなし → Do でエラー
	}

	client := newTestClient(mock)
	_, err := client.ListProjectItems("org", 1, "Status")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Projects アイテムの取得に失敗しました")
}

// ===========================================
// convertItem テスト
// ===========================================

func TestConvertItem_ElapsedDays(t *testing.T) {
	// now を固定して経過日数を正確にテストする
	now := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	node := itemNode{
		Content: contentNode{
			Number: 101,
			Title:  "Test",
			URL:    "https://github.com/org/repo/issues/101",
		},
	}
	field := struct {
		Name string `json:"name"`
	}{"Status"}
	node.FieldValues.Nodes = []fieldValueNode{
		{
			Name:      "In Progress",
			UpdatedAt: "2026-02-20T10:00:00Z",
			Field:     &field,
		},
	}

	item := convertItem(node, "Status", now)
	require.NotNil(t, item)

	// 2026-02-20 → 2026-03-05 = 13日
	assert.Equal(t, 13, item.ElapsedDays)
}

func TestConvertItem_DraftIssueReturnsNil(t *testing.T) {
	now := time.Now()
	node := itemNode{
		Content: contentNode{
			Number: 0, // DraftIssue
			Title:  "Draft",
		},
	}

	item := convertItem(node, "Status", now)
	assert.Nil(t, item)
}

func TestConvertItem_NoStatusChangedAt(t *testing.T) {
	now := time.Now()
	node := itemNode{
		Content: contentNode{
			Number: 1,
			Title:  "No Status",
			URL:    "https://github.com/org/repo/issues/1",
		},
	}

	item := convertItem(node, "Status", now)
	require.NotNil(t, item)
	assert.True(t, item.StatusChangedAt.IsZero())
	assert.Equal(t, 0, item.ElapsedDays)
}

// ===========================================
// FetchStatusTransitions テスト
// ===========================================

func TestFetchStatusTransitions_Basic(t *testing.T) {
	mock := &mockGQL{
		responses: []string{timelineBasicResponse},
	}

	client := newTestClient(mock)
	transitions, err := client.FetchStatusTransitions("https://github.com/example-org/repo/issues/101")
	require.NoError(t, err)
	require.Len(t, transitions, 2) // AddedToProjectEvent はスキップ

	assert.Equal(t, "Todo", transitions[0].From)
	assert.Equal(t, "In Progress", transitions[0].To)
	assert.Equal(t, "In Progress", transitions[1].From)
	assert.Equal(t, "In Review", transitions[1].To)
}

func TestFetchStatusTransitions_EmptyTimeline(t *testing.T) {
	mock := &mockGQL{
		responses: []string{timelineEmptyResponse},
	}

	client := newTestClient(mock)
	transitions, err := client.FetchStatusTransitions("https://github.com/org/repo/issues/1")
	require.NoError(t, err)
	assert.Empty(t, transitions)
}

func TestFetchStatusTransitions_InvalidURL(t *testing.T) {
	client := newTestClient(&mockGQL{})
	_, err := client.FetchStatusTransitions("not-a-url")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URL の解析に失敗しました")
}

func TestFetchStatusTransitions_APIError(t *testing.T) {
	mock := &mockGQL{
		responses: []string{},
	}

	client := newTestClient(mock)
	_, err := client.FetchStatusTransitions("https://github.com/org/repo/issues/1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "タイムラインの取得に失敗しました")
}

// ===========================================
// parseIssueURL テスト
// ===========================================

func TestParseIssueURL_Issue(t *testing.T) {
	owner, repo, number, err := parseIssueURL("https://github.com/example-org/my-repo/issues/123")
	require.NoError(t, err)
	assert.Equal(t, "example-org", owner)
	assert.Equal(t, "my-repo", repo)
	assert.Equal(t, 123, number)
}

func TestParseIssueURL_PullRequest(t *testing.T) {
	owner, repo, number, err := parseIssueURL("https://github.com/org/repo/pull/456")
	require.NoError(t, err)
	assert.Equal(t, "org", owner)
	assert.Equal(t, "repo", repo)
	assert.Equal(t, 456, number)
}

func TestParseIssueURL_TooShort(t *testing.T) {
	_, _, _, err := parseIssueURL("https://github.com/org")
	require.Error(t, err)
}

func TestParseIssueURL_NonNumericNumber(t *testing.T) {
	_, _, _, err := parseIssueURL("https://github.com/org/repo/issues/abc")
	require.Error(t, err)
}

// ===========================================
// テスト用 JSON レスポンス
// ===========================================

const singlePageResponse = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": [
					{
						"id": "item-1",
						"fieldValues": {
							"nodes": [
								{
									"name": "In Progress",
									"updatedAt": "2026-02-20T10:00:00Z",
									"field": {"name": "Status"}
								}
							]
						},
						"content": {
							"number": 101,
							"title": "ユーザー認証API実装",
							"url": "https://github.com/example-org/repo/issues/101",
							"labels": {"nodes": [{"name": "backend"}, {"name": "auth"}]},
							"comments": {"totalCount": 5},
							"assignees": {"nodes": [{"login": "alice"}]}
						}
					},
					{
						"id": "item-2",
						"fieldValues": {
							"nodes": [
								{
									"name": "In Review",
									"updatedAt": "2026-02-15T10:00:00Z",
									"field": {"name": "Status"}
								}
							]
						},
						"content": {
							"number": 102,
							"title": "商品一覧API実装",
							"url": "https://github.com/example-org/repo/issues/102",
							"labels": {"nodes": [{"name": "backend"}]},
							"comments": {"totalCount": 12},
							"assignees": {"nodes": [{"login": "bob"}, {"login": "alice"}]}
						}
					}
				]
			}
		}
	}
}`

const paginationPage1 = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": true, "endCursor": "cursor-page-1"},
				"nodes": [
					{
						"id": "item-1",
						"fieldValues": {"nodes": [{"name": "Todo", "updatedAt": "2026-03-01T00:00:00Z", "field": {"name": "Status"}}]},
						"content": {"number": 1, "title": "Item 1", "url": "https://github.com/org/repo/issues/1", "labels": {"nodes": []}, "comments": {"totalCount": 0}, "assignees": {"nodes": []}}
					}
				]
			}
		}
	}
}`

const paginationPage2 = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": true, "endCursor": "cursor-page-2"},
				"nodes": [
					{
						"id": "item-2",
						"fieldValues": {"nodes": [{"name": "Done", "updatedAt": "2026-03-02T00:00:00Z", "field": {"name": "Status"}}]},
						"content": {"number": 2, "title": "Item 2", "url": "https://github.com/org/repo/issues/2", "labels": {"nodes": []}, "comments": {"totalCount": 0}, "assignees": {"nodes": []}}
					}
				]
			}
		}
	}
}`

const paginationPage3 = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": [
					{
						"id": "item-3",
						"fieldValues": {"nodes": [{"name": "In Progress", "updatedAt": "2026-03-03T00:00:00Z", "field": {"name": "Status"}}]},
						"content": {"number": 3, "title": "Item 3", "url": "https://github.com/org/repo/issues/3", "labels": {"nodes": []}, "comments": {"totalCount": 0}, "assignees": {"nodes": []}}
					}
				]
			}
		}
	}
}`

const emptyProjectResponse = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": []
			}
		}
	}
}`

const draftIssueResponse = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": [
					{
						"id": "item-draft",
						"fieldValues": {"nodes": []},
						"content": {"number": 0, "title": "", "url": ""}
					},
					{
						"id": "item-real",
						"fieldValues": {"nodes": [{"name": "Todo", "updatedAt": "2026-03-01T00:00:00Z", "field": {"name": "Status"}}]},
						"content": {"number": 42, "title": "Real Issue", "url": "https://github.com/org/repo/issues/42", "labels": {"nodes": []}, "comments": {"totalCount": 0}, "assignees": {"nodes": [{"login": "dev"}]}}
					}
				]
			}
		}
	}
}`

const customFieldResponse = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": [
					{
						"id": "item-1",
						"fieldValues": {
							"nodes": [
								{"name": "High", "field": {"name": "Priority"}},
								{"name": "作業中", "updatedAt": "2026-03-01T00:00:00Z", "field": {"name": "進捗"}},
								{"name": "Sprint 1", "field": {"name": "Sprint"}}
							]
						},
						"content": {"number": 10, "title": "カスタムフィールド", "url": "https://github.com/org/repo/issues/10", "labels": {"nodes": []}, "comments": {"totalCount": 0}, "assignees": {"nodes": []}}
					}
				]
			}
		}
	}
}`

const noStatusFieldResponse = `{
	"organization": {
		"projectV2": {
			"items": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": [
					{
						"id": "item-1",
						"fieldValues": {
							"nodes": [
								{"name": "High", "field": {"name": "Priority"}}
							]
						},
						"content": {"number": 10, "title": "No Status", "url": "https://github.com/org/repo/issues/10", "labels": {"nodes": []}, "comments": {"totalCount": 0}, "assignees": {"nodes": []}}
					}
				]
			}
		}
	}
}`

const timelineBasicResponse = `{
	"repository": {
		"issue": {
			"timelineItems": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": [
					{
						"__typename": "MovedColumnsInProjectEvent",
						"createdAt": "2026-02-01T10:00:00Z",
						"previousProjectColumnName": "Todo",
						"projectColumnName": "In Progress"
					},
					{
						"__typename": "MovedColumnsInProjectEvent",
						"createdAt": "2026-02-10T10:00:00Z",
						"previousProjectColumnName": "In Progress",
						"projectColumnName": "In Review"
					},
					{
						"__typename": "AddedToProjectEvent",
						"createdAt": "2026-01-15T10:00:00Z"
					}
				]
			}
		}
	}
}`

const timelineEmptyResponse = `{
	"repository": {
		"issue": {
			"timelineItems": {
				"pageInfo": {"hasNextPage": false, "endCursor": ""},
				"nodes": []
			}
		}
	}
}`
