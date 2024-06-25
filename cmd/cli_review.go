// Copyright 2021 ghstats Project Authors. Licensed under MIT.

package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/go-github/v50/github"
	"github.com/overvenus/ghstats/pkg/config"
	"github.com/overvenus/ghstats/pkg/debug"
	"github.com/overvenus/ghstats/pkg/feishu"
	"github.com/overvenus/ghstats/pkg/gh"
	"github.com/overvenus/ghstats/pkg/markdown"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

const timeFormat = "2006-01-02 15:04:05"

var timeZone *time.Location

func init() {
	timeZone, _ = time.LoadLocation("Asia/Shanghai")
	rootCmd.AddCommand(newReviewCommand())
}

// newReviewCommand returns REVIEW command
func newReviewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "review",
		Short: "Collect daily reviews 👍",
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

			return reviewRange(cmd, "Daily", lastDay, today)
		},
	}

	command.AddCommand(&cobra.Command{
		Use:   "weekly",
		Short: "Collect weekly reviews 👍",
		RunE: func(cmd *cobra.Command, args []string) error {
			today := time.Now().In(timeZone)
			firstDayThisWeek := time.Date(
				today.Year(), today.Month(), today.Day()-int(today.Weekday())+1,
				10, 0, 0, 0, today.Location(),
			)
			// [UTC+8 10:00 on Monday this week, now]
			return reviewRange(cmd, "Weekly", firstDayThisWeek, today)
		},
	})

	command.AddCommand(&cobra.Command{
		Use:   "monthly",
		Short: "Collect monthly reviews 👍",
		RunE: func(cmd *cobra.Command, args []string) error {
			today := time.Now().In(timeZone)
			firstDayLastMonth := time.Date(
				today.Year(), today.Month()-1, today.Day(),
				10, 0, 0, 0, today.Location(),
			)
			// [UTC+8 10:00 on the first day of last month, now]
			return reviewRange(cmd, "Monthly", firstDayLastMonth, today)
		},
	})

	command.AddCommand(&cobra.Command{
		Use:   "debug",
		Short: "Collect reviews within the given time range 📅",
		RunE: func(cmd *cobra.Command, args []string) error {
			start, err := time.Parse(timeFormat, args[0])
			if err != nil {
				return err
			}
			end, err := time.Parse(timeFormat, args[1])
			if err != nil {
				return err
			}
			return reviewRange(cmd, "Debug", start.In(timeZone), end.In(timeZone))
		},
	})

	return command
}

func reviewRange(cmd *cobra.Command, kind string, start, end time.Time) error {
	cfgPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	cfg1, err := config.ReadConfig(cfgPath)
	if err != nil {
		return err
	}
	cfg := cfg1.Review
	c := &reviewConfig{
		lgtmComments:   cfg.LGTMComments,
		blockComments:  cfg.BlockComments,
		blockLabels:    cfg.BlockLabels,
		allowUsers:     make(map[string]bool, len(cfg.AllowUsers)),
		blockUsers:     make(map[string]bool, len(cfg.BlockUsers)),
		startTimestamp: start,
		endTimestamp:   end,
	}
	for i := range cfg.AllowUsers {
		c.allowUsers[cfg.AllowUsers[i]] = true
	}
	for i := range cfg.BlockUsers {
		c.blockUsers[cfg.BlockUsers[i]] = true
	}
	ctx := context.Background()
	client := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cfg.GithubToken},
	)))

	log.Info("review range", start, end)
	current := start
	next := current.Add(24 * time.Hour)
	reviews := make(map[string]review)
	for !(current.Equal(end) || current.After(end)) {
		// Date if formated in time.RFC3339.
		// updated:2021-05-23T21:00:00+08:00..2021-05-24T21:00:00+08:00
		currentRFC3339 := current.Format(time.RFC3339)
		nextRFC3339 := next.Format(time.RFC3339)
		updateRange := fmt.Sprintf("updated:%s..%s", currentRFC3339, nextRFC3339)
		fmt.Printf("[%s] %s -%s\n", time.Now().Format(time.RFC3339), kind, updateRange)
		projects := make(map[string][]*github.IssuesSearchResult)
		for _, proj := range cfg.Repos {
			for _, query := range proj.PRQuery {
				query = fmt.Sprintf("%s %s is:pull-request", strings.TrimSpace(query), updateRange)
				log.Info("query: ", query)
				results, err := gh.SearchIssues(ctx, client, query)
				if err != nil {
					return err
				}
				projects[proj.Name] = append(projects[proj.Name], results...)
			}
		}
		log.Debug("projects issues: ", debug.PrettyFormat(projects))
		for repo, results := range projects {
			_ = repo
			for _, res := range results {
				if len(res.Issues) == 0 {
					continue
				}
				err := collectReviews(ctx, c, client, res.Issues, reviews)
				if err != nil {
					panic(err)
				}
			}
		}
		current = next
		next = current.Add(24 * time.Hour)
		log.Infof("reviews: %v", reviews)
	}

	rs := reviewSlice{}
	for user, r := range reviews {
		rs = append(rs, struct {
			review
			user string
		}{r, user})
	}
	// Highest score ranks first.
	sort.Sort(sort.Reverse(rs))

	// To keep message short we only send top 5 reviewer.
	topN := 5
	buf := strings.Builder{}
	for i, r := range rs {
		user, review := r.user, r.review
		reviewStr := review.String()
		if len(reviewStr) == 0 {
			// The user does not review.
			continue
		}
		trophy := fmt.Sprint("#", i+1)
		switch i {
		case 0:
			trophy = "🏆"
		case 1:
			trophy = "🥈"
		case 2:
			trophy = "🥉"
		}

		userReview := fmt.Sprintf("%s **%s**\n%s\n\n",
			markdown.Escape(trophy), markdown.Escape(user), markdown.Escape(review.String()))
		log.Info(userReview)
		if i >= topN {
			continue
		}
		buf.WriteString(userReview)
	}
	if buf.Len() == 0 {
		buf.WriteString("No reviews 😢")
	}
	buf.WriteString(fmt.Sprintf("\n[%s, %s]", start.Format(timeFormat), end.Format(timeFormat)))
	log.Debug("reviews: ", buf.String())
	bot := feishu.WebhookBot(cfg.FeishuWebhookToken)
	return bot.SendMarkdownMessage(ctx, fmt.Sprintf("Review Top %d 👍 - %s", topN, kind), buf.String(), feishu.TitleColorGreen)
}

type review struct {
	// How many LGTM does one send?
	prLGTMs int
	// How many PR comments does one send?
	prComments int
	// How many issue comments does one send?
	issueComments int
	// How many issues does one create?
	issueCreates int
	// How many labels does one add?
	labelAdds int
}

func (r *review) String() string {
	parts := make([]string, 0)
	if r.prLGTMs != 0 {
		parts = append(parts, fmt.Sprintf("LGTM: %d", r.prLGTMs))
	}
	if r.prComments != 0 {
		parts = append(parts, fmt.Sprintf("PR comments: %d", r.prComments))
	}
	if r.issueComments != 0 {
		parts = append(parts, fmt.Sprintf("issue comments: %d", r.issueComments))
	}
	if r.issueCreates != 0 {
		parts = append(parts, fmt.Sprintf("create issues: %d", r.issueCreates))
	}
	if r.labelAdds != 0 {
		parts = append(parts, fmt.Sprintf("add labels: %d", r.labelAdds))
	}
	return strings.Join(parts, ", ")
}

func (r *review) score() float64 {
	s := 1.0
	if r.prLGTMs != 0 {
		s += float64(r.prLGTMs) * 2.0
	}
	if r.prComments != 0 {
		s += float64(r.prComments) * 1.0
	}
	if r.issueComments != 0 {
		s += float64(r.issueComments) * 1.0
	}
	if r.issueCreates != 0 {
		s += float64(r.issueCreates) * 2.0
	}
	if r.labelAdds != 0 {
		s += float64(r.labelAdds) * 0.5
	}
	return s
}

type reviewSlice []struct {
	review
	user string
}

func (x reviewSlice) Len() int           { return len(x) }
func (x reviewSlice) Less(i, j int) bool { return x[i].score() < x[j].score() }
func (x reviewSlice) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }

type reviewConfig struct {
	lgtmComments   []string
	blockComments  []string
	blockLabels    []string
	allowUsers     map[string]bool
	blockUsers     map[string]bool
	startTimestamp time.Time
	endTimestamp   time.Time
}

// Is the ts within [start, end)?
func (c *reviewConfig) withinTimeRange(ts time.Time) bool {
	ts = ts.In(timeZone)
	return (ts.After(c.startTimestamp) || ts.Equal(c.startTimestamp)) && ts.Before(c.endTimestamp)
}

// If there is an allowing list, check it instead of the block list.
func (c *reviewConfig) isUserBlocked(userLogin string) bool {
	if len(c.allowUsers) > 0 {
		return !c.allowUsers[userLogin]
	}
	return c.blockUsers[userLogin]
}

func (c *reviewConfig) isCommentBlocked(comment string) bool {
	// Unescapes common whitespace in github comments.
	comment = strings.ReplaceAll(comment, "\\n", "\n")
	comment = strings.ReplaceAll(comment, "\\r", "\r")
	comment = strings.ReplaceAll(comment, "\\t", "\t")
	lines := strings.Split(comment, "\n")
	for j := range lines {
		line := strings.TrimSpace(lines[j])
		for i := range c.blockComments {
			if strings.Contains(line, c.blockComments[i]) {
				return true
			}
		}
	}
	return false
}

func (c *reviewConfig) isCommentLGTM(comment string) bool {
	// Unescapes common whitespace in github comments.
	comment = strings.ReplaceAll(comment, "\\n", "\n")
	comment = strings.ReplaceAll(comment, "\\r", "\r")
	comment = strings.ReplaceAll(comment, "\\t", "\t")
	lines := strings.Split(comment, "\n")
	for j := range lines {
		line := strings.TrimSpace(lines[j])
		for i := range c.lgtmComments {
			if c.lgtmComments[i] == line {
				return true
			}
		}
	}
	return false
}

type collector func(
	ctx context.Context,
	c *reviewConfig,
	client *github.Client,
	issues []*github.Issue,
	reviews map[string]review,
) error

// collect reviews for the given issues and PRs.
func collectReviews(
	ctx context.Context,
	c *reviewConfig,
	client *github.Client,
	issues []*github.Issue,
	reviews map[string]review,
) error {
	collectors := []collector{
		collectIssueCreates,
		collectPRLGTM,
		collectPRReviewComments,
		collectIssueAndPRComments,
	}
	for _, collect := range collectors {
		err := collect(ctx, c, client, issues, reviews)
		if err != nil {
			return err
		}
	}
	return nil
}

// Collect review.prLGTM.
// LGTM is an APPROVED PR review or a review summary is LGTM.
func collectPRLGTM(
	ctx context.Context,
	c *reviewConfig,
	client *github.Client,
	issues []*github.Issue,
	reviews map[string]review,
) error {
	for _, issue := range issues {
		if !issue.IsPullRequest() {
			continue
		}
		pr := issue
		owner, repo := gh.GetRepository(pr)
		number := pr.GetNumber()
		prReviews, err := gh.PullRequestsListReviews(ctx, client, owner, repo, number)
		if err != nil {
			return err
		}
		log.Debug("LGTM: ", debug.PrettyFormat(prReviews))
		for _, prReview := range prReviews {
			login := prReview.GetUser().GetLogin()
			if c.isUserBlocked(login) {
				continue
			}
			if login == issue.GetUser().GetLogin() {
				// Do not count author's comments.
				continue
			}
			if !c.withinTimeRange(prReview.GetSubmittedAt().Time) {
				continue
			}
			if prReview.GetState() == "APPROVED" || c.isCommentLGTM(prReview.GetBody()) {
				review := reviews[login]
				review.prLGTMs++
				reviews[login] = review
			}
		}
	}
	return nil
}

// Collect review.prComments.
func collectPRReviewComments(
	ctx context.Context,
	c *reviewConfig,
	client *github.Client,
	issues []*github.Issue,
	reviews map[string]review,
) error {
	for _, issue := range issues {
		if !issue.IsPullRequest() {
			continue
		}
		pr := issue
		owner, repo := gh.GetRepository(pr)
		number := pr.GetNumber()
		prReviews, err := gh.PullRequestsListReviews(ctx, client, owner, repo, number)
		if err != nil {
			return err
		}
		log.Debug("reviews: ", debug.PrettyFormat(prReviews))
		for _, prReview := range prReviews {
			login := prReview.GetUser().GetLogin()
			if c.isUserBlocked(login) {
				continue
			}
			if login == issue.GetUser().GetLogin() {
				// Do not count author's comments.
				continue
			}
			if !c.withinTimeRange(prReview.GetSubmittedAt().Time) {
				continue
			}

			reviewComments, err := gh.PullRequestsListReviewComments(ctx, client, owner, repo, number, *prReview.ID)
			if err != nil {
				return err
			}
			review := reviews[login]
			review.prComments += len(reviewComments)
			reviews[login] = review
		}
	}

	return nil
}

// Collect review.issueComments and review.prComments.
// Also, collect review.prLGTM if a comment is LGTM.
func collectIssueAndPRComments(
	ctx context.Context,
	c *reviewConfig,
	client *github.Client,
	issues []*github.Issue,
	reviews map[string]review,
) error {
	for _, issue := range issues {
		owner, repo := gh.GetRepository(issue)
		number := issue.GetNumber()
		comments, err := gh.IssuesListComments(
			ctx, client, owner, repo, number, &c.startTimestamp)
		if err != nil {
			return err
		}
		log.Debug("comments: ", debug.PrettyFormat(comments))
		for _, comment := range comments {
			login := comment.GetUser().GetLogin()
			if c.isUserBlocked(login) {
				continue
			}
			if login == issue.GetUser().GetLogin() {
				// Do not count author's comments.
				continue
			}
			body := comment.GetBody()
			if c.isCommentBlocked(body) {
				continue
			}
			if c.withinTimeRange(comment.GetCreatedAt().Time) || c.withinTimeRange(comment.GetUpdatedAt().Time) {
				review := reviews[login]
				if issue.IsPullRequest() {
					if c.isCommentLGTM(body) {
						review.prLGTMs++
					} else {
						review.prComments++
					}
				} else {
					review.issueComments++
				}
				reviews[login] = review
			}
		}
	}
	return nil
}

// Collect review.issueCreates.
func collectIssueCreates(
	ctx context.Context,
	c *reviewConfig,
	client *github.Client,
	issues []*github.Issue,
	reviews map[string]review,
) error {
	for _, issue := range issues {
		login := issue.GetUser().GetLogin()
		if c.isUserBlocked(login) {
			continue
		}
		if c.withinTimeRange(issue.GetCreatedAt().Time) {
			review := reviews[login]
			if !issue.IsPullRequest() {
				review.issueCreates++
			}
			reviews[login] = review
		}
	}
	return nil
}
