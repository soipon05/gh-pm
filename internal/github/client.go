// Package github は go-gh ライブラリを使って GitHub Projects v2 API と通信する。
package github

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

//go:embed queries/team_items.graphql
var teamItemsQuery string

// cacheTTL はキャッシュの有効期間。
// 同一セッション内で繰り返し実行しても API を叩き直さない。
const cacheTTL = 30 * time.Minute

type cacheEntry struct {
	FetchedAt time.Time     `json:"fetched_at"`
	Items     []ProjectItem `json:"items"`
}

func cacheFilePath(owner string, projectNumber int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("gh-pm-%s-%d.json", owner, projectNumber))
}

func readCache(owner string, projectNumber int) ([]ProjectItem, bool) {
	data, err := os.ReadFile(cacheFilePath(owner, projectNumber))
	if err != nil {
		return nil, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}
	if time.Since(entry.FetchedAt) > cacheTTL {
		return nil, false
	}
	return entry.Items, true
}

func writeCache(owner string, projectNumber int, items []ProjectItem) {
	data, err := json.Marshal(cacheEntry{FetchedAt: time.Now(), Items: items})
	if err != nil {
		return
	}
	_ = os.WriteFile(cacheFilePath(owner, projectNumber), data, 0600)
}

//go:embed queries/project_items.graphql
var projectItemsQuery string

// gqlClient は GraphQL クエリを実行するインターフェース。
// 本番では go-gh の api.GraphQLClient、テストではモックに差し替える。
//
// Go のインターフェースは「このメソッドを持っていれば OK」という契約。
// go-gh の GraphQLClient は Do メソッドを持っているので、
// 明示的に implements と書かなくても自動的にこのインターフェースを満たす。
type gqlClient interface {
	Do(query string, variables map[string]interface{}, response interface{}) error
}

// --- GraphQL レスポンス型 ---
//
// go-gh の Do メソッドは、レスポンス JSON の "data" フィールドの中身を
// 直接 response パラメータにデシリアライズする。
// つまり "data" ラッパーは不要で、その中の構造だけ定義すればよい。

type projectItemsResponse struct {
	Organization struct {
		ProjectV2 struct {
			Items struct {
				PageInfo pageInfo   `json:"pageInfo"`
				Nodes    []itemNode `json:"nodes"`
			} `json:"items"`
		} `json:"projectV2"`
	} `json:"organization"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type itemNode struct {
	ID          string `json:"id"`
	FieldValues struct {
		Nodes []fieldValueNode `json:"nodes"`
	} `json:"fieldValues"`
	Content contentNode `json:"content"`
}

// fieldValueNode は ProjectV2ItemFieldSingleSelectValue に対応する。
// GraphQL のインラインフラグメント（... on Type）は、型が一致しないノードでは
// フィールドがゼロ値になる。SingleSelect 以外の場合、Field は nil。
type fieldValueNode struct {
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
	Field     *struct {
		Name string `json:"name"`
	} `json:"field"`
}

// contentNode は Issue / PullRequest の共通フィールド。
// DraftIssue の場合、Number は 0 になる（number フィールドがないため）。
type contentNode struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Labels    struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
}

// --- 公開型 ---

// StatusTransition はステータス遷移の1レコード。
// ProjectV2ItemStatusChangedEvent に対応する。
type StatusTransition struct {
	From  string    // 遷移元ステータス
	To    string    // 遷移先ステータス
	At    time.Time // 遷移日時
	Actor string    // 遷移を実行したユーザー
}

// ProjectItem は GitHub Projects の1件のアイテムを表す。
// issue・PR のどちらも同じ構造で扱う。
type ProjectItem struct {
	Number          int                // issue / PR 番号
	Title           string             // タイトル
	URL             string             // issue / PR の URL
	Assignees       []string           // アサイン済みの GitHub ID リスト
	Status          string             // Projects 上の表示名（"In Progress" など）
	StatusCategory  string             // 正規化カテゴリ（"in_progress" など）。呼び出し元が config.CategoryOf() で設定する
	Labels          []string           // ラベル一覧
	CommentCount    int                // コメント数（レビューサイクル分析に使用）
	StatusChangedAt time.Time          // ステータスが最後に変更された日時
	ElapsedDays     int                // 現ステータスでの経過日数
	Transitions     []StatusTransition // ステータス遷移履歴
}

// --- Client ---

// Client は GitHub Projects API のラッパー。
// go-gh の GraphQL クライアントをラップして、このツール向けの型で返す。
type Client struct {
	gql     gqlClient
	noCache bool // テスト時は true にしてキャッシュをバイパスする
}

// NewClient は go-gh の認証情報を使って Client を生成する。
//
// gh auth login が完了していれば、トークンは自動で取得される。
// ユーザーがトークンを意識しなくてよいのは go-gh のおかげ。
func NewClient() (*Client, error) {
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("GitHub API クライアントの作成に失敗しました: %w", err)
	}
	return &Client{gql: client}, nil
}

// newTestClient はテスト用の Client を生成する。モック GQL クライアントを注入できる。
func newTestClient(gql gqlClient) *Client {
	return &Client{gql: gql, noCache: true}
}

// itemsPerPage は1回の API リクエストで取得するアイテム数。
// GitHub GraphQL API の上限は 100。
const itemsPerPage = 100

// ListProjectItems は GitHub Projects の全アイテムを取得して返す。
//
// ページネーション（100 件ずつ）を自動で処理し、1000 件超のプロジェクトでも全件取得する。
// statusFieldName は Projects 上の Status フィールド名（通常は "Status"）。
// このフィールドの値から Status と StatusChangedAt を抽出する。
//
// 注意: StatusCategory は設定されない。呼び出し元が config.CategoryOf() で設定すること。
func (c *Client) ListProjectItems(owner string, projectNumber int, statusFieldName string) ([]ProjectItem, error) {
	if !c.noCache {
		if cached, ok := readCache(owner, projectNumber); ok {
			return cached, nil
		}
	}

	var allItems []ProjectItem
	var cursor interface{} // 最初は nil（= GraphQL の null）
	now := time.Now()

	for {
		variables := map[string]interface{}{
			"owner":  owner,
			"number": projectNumber,
			"first":  itemsPerPage,
			"after":  cursor,
		}

		var resp projectItemsResponse
		if err := c.gql.Do(projectItemsQuery, variables, &resp); err != nil {
			return nil, fmt.Errorf("Projects アイテムの取得に失敗しました: %w", err)
		}

		items := resp.Organization.ProjectV2.Items
		for _, node := range items.Nodes {
			item := convertItem(node, statusFieldName, now)
			if item != nil {
				allItems = append(allItems, *item)
			}
		}

		if !items.PageInfo.HasNextPage {
			break
		}
		cursor = items.PageInfo.EndCursor
	}

	if !c.noCache {
		writeCache(owner, projectNumber, allItems)
	}
	return allItems, nil
}

// convertItem は GraphQL レスポンスの1ノードを ProjectItem に変換する。
// DraftIssue（Number が 0）の場合は nil を返してスキップする。
func convertItem(node itemNode, statusFieldName string, now time.Time) *ProjectItem {
	content := node.Content
	if content.Number == 0 {
		return nil // DraftIssue はスキップ
	}

	// fieldValues から Status フィールドの値を探す
	// fieldValues には Status 以外のフィールド（Priority, Sprint など）も含まれるので、
	// フィールド名で絞り込む
	var status string
	var statusChangedAt time.Time
	for _, fv := range node.FieldValues.Nodes {
		if fv.Field != nil && fv.Field.Name == statusFieldName {
			status = fv.Name
			if fv.UpdatedAt != "" {
				statusChangedAt, _ = time.Parse(time.RFC3339, fv.UpdatedAt)
			}
			break
		}
	}

	// Assignees を抽出
	assignees := make([]string, 0, len(content.Assignees.Nodes))
	for _, a := range content.Assignees.Nodes {
		assignees = append(assignees, a.Login)
	}

	// Labels を抽出
	labels := make([]string, 0, len(content.Labels.Nodes))
	for _, l := range content.Labels.Nodes {
		labels = append(labels, l.Name)
	}

	// 現ステータスでの経過日数を計算
	var elapsedDays int
	if !statusChangedAt.IsZero() {
		elapsedDays = int(now.Sub(statusChangedAt).Hours() / 24)
	}

	return &ProjectItem{
		Number:          content.Number,
		Title:           content.Title,
		URL:             content.URL,
		Assignees:       assignees,
		Status:          status,
		Labels:          labels,
		CommentCount:    content.Comments.TotalCount,
		StatusChangedAt: statusChangedAt,
		ElapsedDays:     elapsedDays,
	}
}

// --- チーム高速パス（GitHub Search API ベース） ---

// searchNode は GitHub Search API のレスポンスの1ノード。
// Issue / PullRequest 共通フィールドを持つ。
type searchNode struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
	ProjectItems struct {
		Nodes []struct {
			Project struct {
				Number int `json:"number"`
			} `json:"project"`
			FieldValues struct {
				Nodes []fieldValueNode `json:"nodes"`
			} `json:"fieldValues"`
		} `json:"nodes"`
	} `json:"projectItems"`
}

type searchResponse struct {
	Search struct {
		PageInfo pageInfo     `json:"pageInfo"`
		Nodes    []searchNode `json:"nodes"`
	} `json:"search"`
}

// teamCacheFilePath はチーム高速パス用のキャッシュファイルパスを返す。
func teamCacheFilePath(owner string, projectNumber int, members []string) string {
	key := fmt.Sprintf("gh-pm-%s-%d-team-%s.json", owner, projectNumber, strings.Join(members, "_"))
	return filepath.Join(os.TempDir(), key)
}

func readTeamCache(path string) ([]ProjectItem, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}
	if time.Since(entry.FetchedAt) > cacheTTL {
		return nil, false
	}
	return entry.Items, true
}

func writeTeamCache(path string, items []ProjectItem) {
	data, err := json.Marshal(cacheEntry{FetchedAt: time.Now(), Items: items})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

// ListTeamItems はチームメンバーを GitHub Search で絞り込んで高速取得する。
// チーム指定時の高速パス。ListProjectItems（全件スキャン）の代替として使用する。
func (c *Client) ListTeamItems(owner string, projectNumber int, members []string, statusFieldName string) ([]ProjectItem, error) {
	cachePath := teamCacheFilePath(owner, projectNumber, members)
	if !c.noCache {
		if cached, ok := readTeamCache(cachePath); ok {
			return cached, nil
		}
	}

	now := time.Now()
	seen := map[int]bool{}
	var allItems []ProjectItem

	// "assignee:m1 assignee:m2 ..." は GitHub Search では OR 条件になる
	assigneeFilter := ""
	for _, m := range members {
		assigneeFilter += "assignee:" + m + " "
	}

	// Open issues: In Progress / In Review / Done(プロジェクト上) だが GitHub 上は open のもの
	openItems, err := c.searchProjectItems(
		fmt.Sprintf("org:%s %sis:open", owner, assigneeFilter),
		projectNumber, statusFieldName, now,
	)
	if err != nil {
		return nil, err
	}
	for _, item := range openItems {
		if !seen[item.Number] {
			seen[item.Number] = true
			allItems = append(allItems, item)
		}
	}

	// Recently closed issues: done_last_7days の計算用に直近30日分を取得
	since := now.AddDate(0, 0, -30).Format("2006-01-02")
	closedItems, err := c.searchProjectItems(
		fmt.Sprintf("org:%s %sis:closed updated:>%s", owner, assigneeFilter, since),
		projectNumber, statusFieldName, now,
	)
	if err != nil {
		return nil, err
	}
	for _, item := range closedItems {
		if !seen[item.Number] {
			seen[item.Number] = true
			allItems = append(allItems, item)
		}
	}

	if !c.noCache {
		writeTeamCache(cachePath, allItems)
	}
	return allItems, nil
}

// searchProjectItems は1つの検索クエリを実行してプロジェクトアイテムを返す。
func (c *Client) searchProjectItems(searchQuery string, projectNumber int, statusFieldName string, now time.Time) ([]ProjectItem, error) {
	var allItems []ProjectItem
	var cursor interface{}

	for {
		variables := map[string]interface{}{
			"searchQuery": searchQuery,
			"first":       itemsPerPage,
			"after":       cursor,
		}

		var resp searchResponse
		if err := c.gql.Do(teamItemsQuery, variables, &resp); err != nil {
			return nil, fmt.Errorf("チームアイテムの検索に失敗しました: %w", err)
		}

		for _, node := range resp.Search.Nodes {
			item := convertSearchNode(node, projectNumber, statusFieldName, now)
			if item != nil {
				allItems = append(allItems, *item)
			}
		}

		if !resp.Search.PageInfo.HasNextPage {
			break
		}
		cursor = resp.Search.PageInfo.EndCursor
	}

	return allItems, nil
}

// convertSearchNode は検索結果ノードを ProjectItem に変換する。
// 指定プロジェクトに含まれないアイテムは nil を返してスキップする。
func convertSearchNode(node searchNode, projectNumber int, statusFieldName string, now time.Time) *ProjectItem {
	if node.Number == 0 {
		return nil
	}

	// 指定プロジェクトの projectItem を探してステータスを取得
	var status string
	var statusChangedAt time.Time
	found := false

	for _, pi := range node.ProjectItems.Nodes {
		if pi.Project.Number != projectNumber {
			continue
		}
		found = true
		for _, fv := range pi.FieldValues.Nodes {
			if fv.Field != nil && fv.Field.Name == statusFieldName {
				status = fv.Name
				if fv.UpdatedAt != "" {
					statusChangedAt, _ = time.Parse(time.RFC3339, fv.UpdatedAt)
				}
				break
			}
		}
		break
	}

	if !found {
		return nil // 指定プロジェクトに含まれないアイテムはスキップ
	}

	assignees := make([]string, 0, len(node.Assignees.Nodes))
	for _, a := range node.Assignees.Nodes {
		assignees = append(assignees, a.Login)
	}

	labels := make([]string, 0, len(node.Labels.Nodes))
	for _, l := range node.Labels.Nodes {
		labels = append(labels, l.Name)
	}

	var elapsedDays int
	if !statusChangedAt.IsZero() {
		elapsedDays = int(now.Sub(statusChangedAt).Hours() / 24)
	}

	return &ProjectItem{
		Number:          node.Number,
		Title:           node.Title,
		URL:             node.URL,
		Assignees:       assignees,
		Status:          status,
		Labels:          labels,
		CommentCount:    node.Comments.TotalCount,
		StatusChangedAt: statusChangedAt,
		ElapsedDays:     elapsedDays,
	}
}
