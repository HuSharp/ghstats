// Copyright 2021 ghstats Project Authors. Licensed under MIT.

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v50/github"
	"github.com/overvenus/ghstats/pkg/config"
	"github.com/overvenus/ghstats/pkg/feishu"
	"github.com/overvenus/ghstats/pkg/gh"
	"github.com/overvenus/ghstats/pkg/markdown"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

func init() {
	rootCmd.AddCommand(newPTALCommand())
}

// newCommand returns PTAL command
func newPTALCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "ptal",
		Short: "Please take a look Pull Requests ❤️",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfg1, err := config.ReadConfig(cfgPath)
			if err != nil {
				return err
			}
			cfg := cfg1.PTAL
			ctx := context.Background()
			client := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: cfg.GithubToken},
			)))

			projects := make(map[string][]*github.IssuesSearchResult)
			for _, proj := range cfg.Repos {
				for _, query := range proj.PRQuery {
					results, err := gh.SearchIssues(ctx, client, fmt.Sprintf("repo:%s is:pull-request", strings.TrimSpace(query)))
					if err != nil {
						return err
					}
					projects[proj.Name] = append(projects[proj.Name], results...)
				}
			}
			buf := strings.Builder{}
			// To keep message short, we only keep the most recent 10 PRs.
			max := 10
			for repo, results := range projects {
				count := 0
				prs := strings.Builder{}
				for _, res := range results {
					for _, issue := range res.Issues {
						if count > max {
							break
						}
						// do not find a unify label to identify "WIP" status, so just check the title for now
						if strings.Contains(strings.ToLower(*issue.Title), "wip") {
							continue
						}
						// So as to "DNM"
						if strings.Contains(strings.ToLower(*issue.Title), "dnm") {
							continue
						}
						prs.WriteString(fmt.Sprintf("%s %s\n",
							markdown.Link(fmt.Sprintf("#%d", *issue.Number), *issue.HTMLURL),
							markdown.Escape(*issue.Title),
						))
						count++
					}
				}
				if prs.Len() != 0 {
					buf.WriteString(fmt.Sprintf("## %s\n", markdown.Escape(repo)))
					buf.WriteString(prs.String())
				}
			}
			if buf.Len() == 0 {
				// Good! No PR need to be reviewed.
				return nil
			}
			bot := feishu.WebhookBot(cfg.FeishuWebhookToken)
			return bot.SendMarkdownMessage(ctx, "PTAL ❤️", buf.String(), feishu.TitleColorWathet)
		},
	}
	return command
}
