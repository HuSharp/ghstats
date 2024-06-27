package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v50/github"
	"github.com/overvenus/ghstats/cmd/ci_test"
	"github.com/overvenus/ghstats/pkg/config"
	"github.com/overvenus/ghstats/pkg/feishu"
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
			today := time.Now().In(timeZone)
			lastDay := today
			switch today.Weekday() {
			// Monday, collect past 3 days review activity.
			case time.Monday:
				lastDay = lastDay.Add(-3 * 24 * time.Hour)
			// Others, collect past 1 day review activity.
			default:
				lastDay = lastDay.Add(-24 * time.Hour)
			}
			return checkTestRange(cmd, "Daily", lastDay)
		},
	}

	command.AddCommand(&cobra.Command{
		Use:   "weekly",
		Short: "Collect weekly test status",
		RunE: func(cmd *cobra.Command, args []string) error {
			lastDay := time.Now().In(timeZone).Add(-7 * 24 * time.Hour)
			return checkTestRange(cmd, "Weekly", lastDay)
		},
	})

	return command
}

func checkTestRange(cmd *cobra.Command, kind string, lastDay time.Time) error {
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

	githubClient := getGithubClient(cfg.GithubToken)
	prLists := getPRBetweenMergedTime(githubClient, owner, repo, lastDay)

	// build sha map
	shaMap := make(map[string]bool)
	for _, pr := range prLists {
		allCommits := getPRAllCommits(githubClient, owner, repo, pr.GetNumber())
		for _, commit := range allCommits {
			shaMap[commit.GetSHA()] = true
		}
	}

	// ger failed ci link
	buf := strings.Builder{}
	_ = getFailedCIURLWithCommits(githubClient, owner, repo, shaMap)

	fmt.Println("======Below is the unstable ut ci link======")
	for failedName, stats := range ci_test.TestNameMap {
		printContent := fmt.Sprintf("%s\n"+
			"failed count: %d, link: %s\n\n", failedName, stats.FailedCount, stats.CILink)
		fmt.Println(printContent)
		buf.WriteString(printContent)
	}

	now := time.Now().In(timeZone)
	buf.WriteString(fmt.Sprintf("\n[%s, %s]", lastDay.Format(timeFormat), now.Format(timeFormat)))

	bot := feishu.WebhookBot(cfg.FeishuWebhookToken)
	ctx := context.Background()
	return bot.SendMarkdownMessage(ctx, fmt.Sprintf("Check Test Status️(%s)", kind),
		buf.String(), feishu.TitleColorWathet)
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

func getPRBetweenMergedTime(client *github.Client, owner string, repo string, lastDay time.Time) (prs []*github.PullRequest) {
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
			// get PRs which is updated 1 days ago
			if pr.GetUpdatedAt().Before(lastDay) {
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

func getPRAllCommits(client *github.Client, owner string, repo string, prNumber int) (allCommits []*github.RepositoryCommit) {
	opt := &github.ListOptions{PerPage: 100}

	for {
		commits, resp, err := client.PullRequests.ListCommits(context.Background(), owner, repo, prNumber, opt)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		allCommits = append(allCommits, commits...)
		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	return
}

func getFailedCIURLWithCommits(client *github.Client, owner string, repo string,
	commitsMap map[string]bool) (ciLists []string) {
	checkType := []string{"jenkins", "action"}
	for _, check := range checkType {
		switch check {
		case "jenkins":
			// fmt.Println("get jenkins failed ci link")
			// ciLists = append(ciLists, ci_test.GetJenkinsFailed(commitsMap)...)
		case "action":
			fmt.Println("get action failed ci link")
			ciLists = append(ciLists, ci_test.GetActionFailed(client, owner, repo, commitsMap)...)
		}
	}
	return
}
