package ci_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-github/v50/github"
)

var ignoreNames = []string{"codecov", "report-coverage", "DeepSource", "DCO", "statics"}
var TestNameMap = make(map[string]*testStats)

type testStats struct {
	Name        string
	CILink      []string
	FailedCount int
}

func GetActionFailed(client *github.Client, owner string, repo string, commitsMap map[string]bool) (ciLists []string) {
	ctx := context.Background()
	// List all pull requests
	for sha := range commitsMap {
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
				url, _, _ := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, *checkRun.ID, true)
				if url != nil {
					getCheckLog(url.String(), *checkRun.HTMLURL)
				}
				ciLists = append(ciLists, *checkRun.HTMLURL)
			}
		}
	}
	return
}

// https://github.com/tikv/pd/commit/5701d249e13c773098c0c86aa80494d9690f5d9a/checks/26446422026/logs
func getCheckLog(logsURL, ciLink string) {
	response, err := http.Get(logsURL)
	if err != nil {
		fmt.Printf("get check log failed: %v\n", err)
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Printf("read check log failed: %v\n", err)
		return
	}

	var printContent []string
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		// format is `2024-06-21T02:53:34.8525620Z --- FAIL: TestHTTPClientTestSuiteWithServiceDiscovery (22.02s)`
		if strings.Contains(line, "FAIL: Test") {
			// if next line contains `FAIL: Test`, then skip this line
			if strings.Contains(lines[i+1], "FAIL: Test") {
				continue
			}
			// check if the test name is valid
			testName := strings.Split(strings.Split(line, "FAIL: ")[1], " ")[0]
			fmt.Println("TestName: ", testName)
			if !strings.Contains(testName, "Test") {
				fmt.Println("test name is not valid: ", testName, logsURL)
				continue
			}
			// update test stats
			if _, ok := TestNameMap[testName]; !ok {
				TestNameMap[testName] = &testStats{
					Name:        testName,
					FailedCount: 1,
					CILink:      []string{ciLink},
				}
			} else {
				TestNameMap[testName].CILink = append(TestNameMap[testName].CILink, ciLink)
				TestNameMap[testName].FailedCount++
			}

			printContent = append(printContent, line)
			continue
		}
		
		if printContent != nil {
			printContent = append(printContent, line)
			if strings.Contains(line, "FAIL") {
				break
			}
		}

	}

	fmt.Println("======Below is the failed test content======")
	// fmt.Println("logURL: ", logsURL)
	fmt.Println(strings.Join(printContent, "\n"))
}
