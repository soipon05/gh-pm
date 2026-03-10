package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/soipon05/gh-pm/internal/config"
	gh "github.com/soipon05/gh-pm/internal/github"
	"github.com/soipon05/gh-pm/internal/render"
	"github.com/spf13/cobra"
)

type standupFlags struct {
	team    string
	noColor bool
	refresh bool
}

func newStandupCmd() *cobra.Command {
	flags := &standupFlags{}

	cmd := &cobra.Command{
		Use:   "standup [team]",
		Short: "今日のスタンドアップ情報を表示する",
		Long: `チームメンバーのWIP・レビュー待ち・直近完了をまとめて表示する。
朝会の前に打つだけで全員の状況が把握できる。

メンバーのレベル（junior/mid/senior）に応じて声かけ推奨を表示する。
.gpm.yml の teams.<name>.levels でレベルを設定できる。`,
		Example: `  gh pm standup              # 全チームのスタンドアップ
  gh pm standup backend      # backend チームのみ
  gh pm standup --refresh    # キャッシュを無視して最新取得`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			team := ""
			if len(args) == 1 {
				team = args[0]
			}
			return runStandup(appConfig, team, flags)
		},
	}

	cmd.Flags().StringVar(&flags.team, "team", "", "チーム名で絞り込む（引数でも指定可）")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "カラー出力を無効化する")
	cmd.Flags().BoolVar(&flags.refresh, "refresh", false, "キャッシュを無視して最新データを取得")

	return cmd
}

func runStandup(cfg *config.Config, team string, flags *standupFlags) error {
	var (
		client *gh.Client
		err    error
	)
	if flags.refresh {
		client, err = gh.NewClientWithRefresh()
	} else {
		client, err = gh.NewClient()
	}
	if err != nil {
		return err
	}

	fmt.Fprint(os.Stderr, "取得中...")

	// チーム指定 or 全チーム
	var items []gh.ProjectItem
	if team != "" {
		if _, ok := cfg.Teams[team]; !ok {
			fmt.Fprintln(os.Stderr)
			return fmt.Errorf("チーム %q が設定ファイルに見つかりません。\n  設定されているチーム: %s", team, teamNames(cfg))
		}
		items, _, err = client.ListTeamItems(
			cfg.Project.Owner, cfg.Project.Number,
			cfg.Teams[team].Members, cfg.Fields.Status.Name,
		)
	} else if members := allUniqueMembers(cfg); len(members) > 0 {
		items, _, err = client.ListTeamItems(
			cfg.Project.Owner, cfg.Project.Number,
			members, cfg.Fields.Status.Name,
		)
	} else {
		items, err = client.ListProjectItems(cfg.Project.Owner, cfg.Project.Number, cfg.Fields.Status.Name)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}
	fmt.Fprintln(os.Stderr, " 完了")

	// StatusCategory をマッピング
	for i := range items {
		items[i].StatusCategory = cfg.CategoryOf(items[i].Status)
	}

	// チーム絞り込み
	if team != "" {
		items = filterByTeam(items, cfg, team)
	}

	// メンバー一覧を取得（表示順: team設定順）
	memberList := collectMembers(cfg, team)

	// メンバーごとにアイテムをグループ化
	standupMembers := buildStandupMembers(items, memberList, cfg)

	// 表示
	render.PrintStandup(standupMembers, flags.noColor)
	render.PrintStandupFooter(flags.noColor)

	return nil
}

// collectMembers は表示するメンバーのリストをチーム設定から取得する。
func collectMembers(cfg *config.Config, team string) []string {
	if team != "" {
		return cfg.Teams[team].Members
	}

	// 全チームのメンバーをソート順で収集（重複除去）
	names := make([]string, 0, len(cfg.Teams))
	for name := range cfg.Teams {
		names = append(names, name)
	}
	sort.Strings(names)

	seen := map[string]bool{}
	var members []string
	for _, name := range names {
		for _, m := range cfg.Teams[name].Members {
			if !seen[m] {
				seen[m] = true
				members = append(members, m)
			}
		}
	}
	return members
}

// buildStandupMembers はアイテムリストからスタンドアップ表示用のメンバー情報を構築する。
func buildStandupMembers(items []gh.ProjectItem, memberList []string, cfg *config.Config) []render.StandupMember {
	// メンバーごとにアイテムを振り分け
	inProgress := map[string][]render.Item{}
	inReview := map[string][]render.Item{}
	recentDone := map[string][]render.Item{}
	nextTodo := map[string][]render.Item{}

	for _, item := range items {
		ri := render.Item{
			Number:         item.Number,
			Title:          item.Title,
			Assignees:      item.Assignees,
			Status:         item.Status,
			StatusCategory: item.StatusCategory,
			ElapsedDays:    item.ElapsedDays,
		}

		for _, assignee := range item.Assignees {
			switch item.StatusCategory {
			case "in_progress":
				inProgress[assignee] = append(inProgress[assignee], ri)
			case "in_review":
				inReview[assignee] = append(inReview[assignee], ri)
			case "done":
				// ElapsedDaysが0〜1日のものを「直近完了」として扱う
				if item.ElapsedDays <= 1 {
					recentDone[assignee] = append(recentDone[assignee], ri)
				}
			case "todo":
				// 最初の1件だけ
				if len(nextTodo[assignee]) == 0 {
					nextTodo[assignee] = append(nextTodo[assignee], ri)
				}
			}
		}
	}

	// StandupMember を構築（memberList の順序で）
	members := make([]render.StandupMember, 0, len(memberList))
	for _, id := range memberList {
		members = append(members, render.StandupMember{
			GitHubID:   id,
			Level:      cfg.MemberLevelOf(id),
			InProgress: inProgress[id],
			InReview:   inReview[id],
			RecentDone: recentDone[id],
			NextTodo:   nextTodo[id],
		})
	}

	return members
}
