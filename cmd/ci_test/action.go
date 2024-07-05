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
	CILink      map[string]int
	FailedCount int
}

func GetActionFailed(client *github.Client, owner string, repo string, commitsMap map[string]int) (ciLists []string) {
	ctx := context.Background()
	// List all pull requests
	status, all := "completed", "all"
	for sha, prNumber := range commitsMap {
		// List check runs for each pull request
		checkRuns, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, &github.ListCheckRunsOptions{
			Filter: &all,
			Status: &status,
		})
		if err != nil {
			fmt.Println(err)
			return
		}
		println("sha: ", sha, " prNumber: ", prNumber, " checkRuns: ", len(checkRuns.CheckRuns))

	check:
		for _, checkRun := range checkRuns.CheckRuns {
			for _, ignoreName := range ignoreNames {
				if strings.Contains(*checkRun.Name, ignoreName) {
					continue check
				}
			}
			if checkRun.Conclusion != nil && strings.Contains(*checkRun.Conclusion, "failure") {
				url, _, _ := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, *checkRun.ID, true)
				if url != nil {
					getCheckLog(url.String(), *checkRun.HTMLURL, prNumber)
				}
				ciLists = append(ciLists, *checkRun.HTMLURL)
			}
		}
	}
	return
}

// https://github.com/tikv/pd/commit/5701d249e13c773098c0c86aa80494d9690f5d9a/checks/26446422026/logs
func getCheckLog(logsURL, ciLink string, prNumber int) {
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
		// format is `2024-06-20T03:58:04.8182577Z --- FAIL: TestTSOKeyspaceGroupManager (56.79s)`
		if strings.Contains(line, "FAIL: Test") {
			//
			/*
				when found `Suite`, then skip this line.
				The suite format is like:
				- `--- FAIL: TestTSOKeyspaceGroupManager/TestKeyspaceGroupMergeIntoDefault`
				or
				- `--- FAIL: TestRuleTestSuite (34.05s)
				    testutil.go:319: start test TestBatch in pd mode
					...
					testutil.go:349: start test TestLeaderAndVoter in api mode
				   	--- FAIL: TestRuleTestSuite/TestLeaderAndVoter (20.35s)`

			*/
			if strings.Contains(lines[i+1], "Test") || strings.Contains(lines[i+1], "/") {
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
					CILink:      map[string]int{ciLink: prNumber},
				}
			} else {
				TestNameMap[testName].CILink[ciLink] = prNumber
				TestNameMap[testName].FailedCount++
			}

			printContent = append(printContent, line)
			continue
		}

		if len(printContent) != 0 {
			printContent = append(printContent, line)
			if strings.Contains(line, "FAIL") {
				break
			}
		}
	}

	if len(printContent) == 0 {
		for i, line := range lines {
			if strings.Contains(line, "goleak") {
				testName := "goleak"
				if _, ok := TestNameMap[testName]; !ok {
					TestNameMap[testName] = &testStats{
						Name:        testName,
						FailedCount: 1,
						CILink:      map[string]int{ciLink: prNumber},
					}
				} else {
					TestNameMap[testName].CILink[ciLink] = prNumber
					TestNameMap[testName].FailedCount++
				}
				fmt.Println("goleak found: ", logsURL)
				printContent = append(printContent, line)
				for j := i + 1; j < len(lines); j++ {
					if !strings.Contains(lines[j], "make: ***") {
						printContent = append(printContent, lines[j])
					}
				}
				break
			}

		}
	}

	fmt.Println("======Below is the failed test content======")
	fmt.Println(strings.Join(printContent, "\n"))
}
