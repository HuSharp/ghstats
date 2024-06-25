package ci_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v32/github"
)

var ignoreNames = []string{"codecov", "report-coverage", "DeepSource", "DCO"}

func GetActionFailed(client *github.Client, owner string, repo string, commitsMap map[string]bool) (ciLists []string) {
	ctx := context.Background()
	// List all pull requests
	for sha, _ := range commitsMap {
		// List check runs for each pull request
		checkRuns, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, &github.ListCheckRunsOptions{})
		if err != nil {
			fmt.Println(err)
			return
		}

	check:
		for _, checkRun := range checkRuns.CheckRuns {
			for _, ignoreName := range ignoreNames {
				if strings.Contains(*checkRun.Name, ignoreName) {
					continue check
				}
			}
			if strings.Contains(*checkRun.Conclusion, "failure") {
				fmt.Println("Pull Request: ", sha, " Check Run: ", *checkRun.Name, " Conclusion: ", *checkRun.Conclusion)
				ciLists = append(ciLists, *checkRun.HTMLURL)
			}
		}
	}
	return
}
