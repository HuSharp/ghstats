package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/overvenus/ghstats/pkg/config"
	"github.com/overvenus/ghstats/pkg/feishu"

	"github.com/google/go-github/v32/github"
	"github.com/overvenus/ghstats/cmd/ci_test"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

func init() {
	timeZone, _ = time.LoadLocation("Asia/Shanghai")
	rootCmd.AddCommand(newTestCommand())
}

// newReviewCommand returns TEST command
func newTestCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "test",
		Short: "Collect daily test status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfg1, err := config.ReadConfig(cfgPath)
			if err != nil {
				return err
			}
			// using review config
			cfg := cfg1.Review
			// get pr before 7 days
			githubClient := getGithubClient(cfg.GithubToken)
			prLists := getPRBetweenMergedTime(githubClient, owner, repo)

			// build sha map
			shaMap := make(map[string]bool)
			for _, pr := range prLists {
				shaKey := *getPRLastCommit(githubClient, owner, repo, pr.GetNumber())
				shaMap[shaKey] = true
			}

			// ger failed ci link
			buf := strings.Builder{}
			ciLists := getFailedCIURLWithCommits(githubClient, owner, repo, shaMap)

			fmt.Println("======Below is the unstable ut ci link======")
			for _, ciLink := range ciLists {
				fmt.Println("ci link: ", ciLink)
				buf.WriteString(fmt.Sprintf("ci link: %s\n", ciLink))
			}

			bot := feishu.WebhookBot(cfg.FeishuWebhookToken)
			ctx := context.Background()
			return bot.SendMarkdownMessage(ctx, "check test status️", buf.String(), feishu.TitleColorWathet)
		},
	}

	return command
}

const (
	owner = "tikv"
	repo  = "pd"
)

func getGithubClient(githubToken string) *github.Client {
	// github token
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return client
}

func getPRBetweenMergedTime(client *github.Client, owner string, repo string) (prs []*github.PullRequest) {
	now := time.Now()
	opt := &github.PullRequestListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		State:       "closed",
		Base:        "master",
		Sort:        "updated",
		Direction:   "desc",
	}

	var prLists []*github.PullRequest

	for {
		PRs, resp, err := client.PullRequests.List(context.Background(), owner, repo, opt)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		for _, pr := range PRs {
			fmt.Println("PR number: ", pr.GetNumber(), " which is merged at: ", pr.GetMergedAt(), " and updated at: ", pr.GetUpdatedAt())
			prLists = append(prLists, pr)
			// get PRs which is updated 7 days ago
			if pr.GetUpdatedAt().Before(now.AddDate(0, 0, -14)) {
				return prLists
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	return prLists

}

func getPRLastCommit(client *github.Client, owner string, repo string, prNumber int) *string {
	opt := &github.ListOptions{PerPage: 100}

	var commit string

	for {
		commits, resp, err := client.PullRequests.ListCommits(context.Background(), owner, repo, prNumber, opt)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		commit = commits[len(commits)-1].GetSHA()

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	return &commit
}

func getFailedCIURLWithCommits(client *github.Client, owner string, repo string,
	commitsMap map[string]bool) (ciLists []string) {
	checkType := []string{"jenkins", "action"}
	for _, check := range checkType {
		switch check {
		case "jenkins":
			fmt.Println("get jenkins failed ci link")
			ciLists = append(ciLists, ci_test.GetJenkinsFailed(commitsMap)...)
		case "action":
			fmt.Println("get action failed ci link")
			ciLists = append(ciLists, ci_test.GetActionFailed(client, owner, repo, commitsMap)...)
		}
	}
	return
}
