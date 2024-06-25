package ci_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type buildObject struct {
	Number int    `json:"number"`
	Url    string `json:"url"`
}
type JenkinsResponse struct {
	Builds []buildObject `json:"builds"`
}

type Pull struct {
	Number int    `json:"number"`
	Author string `json:"author"`
	Commit string `json:"sha"`
	Title  string `json:"title"`
}

type Ref struct {
	Pull    []Pull `json:"pulls"`
	BaseSHA string `json:"base_sha"`
}
type JobSpec struct {
	Refs Ref `json:"refs"`
}
type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Action struct {
	Parameters []Parameter `json:"parameters"`
}

type JobInfo struct {
	Actions []Action `json:"actions"`
	Result  string   `json:"result"`
}

// only support `real-cluster` now
var jenkinsURL = "https://do.pingcap.net/jenkins/job/tikv/job/pd/job/pull_integration_realcluster_test//"

func GetJenkinsFailed(commitsMap map[string]bool) (ciLists []string) {
	verifyJenkinsResponse, err := http.Get(jenkinsURL + "api/json")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer verifyJenkinsResponse.Body.Close()
	verifyJenkinsBodyBytes, err := io.ReadAll(verifyJenkinsResponse.Body)
	if err != nil {
		log.Fatalf("Failed to read verify response body: %v", err)
	}

	var jenkinsResponse JenkinsResponse
	if jsonErr := json.Unmarshal(verifyJenkinsBodyBytes, &jenkinsResponse); jsonErr != nil {
		log.Fatalf("Failed to unmarshal jenkins response: %v", jsonErr)
	}

	count := 0
	for _, build := range jenkinsResponse.Builds {
		count += 1

		buildJobUrl := fmt.Sprintf(jenkinsURL+"%d/api/json", build.Number)
		buildJobResp, err := http.Get(buildJobUrl)
		if err != nil {
			fmt.Println(err)
			return
		}
		buildJobRespBody, err := io.ReadAll(buildJobResp.Body)
		if err != nil {
			log.Fatalf("Failed to read build job response body: %v", err)
		}

		var jobInfo JobInfo
		if jsonErr := json.Unmarshal(buildJobRespBody, &jobInfo); jsonErr != nil {
			log.Fatalf("Failed to unmarshal build job response: %v", jsonErr)
		}

		if jobInfo.Result != "FAILURE" {
			continue
		}

		parameterIndex := len(jobInfo.Actions[0].Parameters)
		jobSpecValue := jobInfo.Actions[0].Parameters[parameterIndex-1].Value

		var jobSpec JobSpec
		if jsonErr := json.Unmarshal([]byte(jobSpecValue), &jobSpec); jsonErr != nil {
			log.Fatalf("Failed to unmarshal job spec response: %v", jsonErr)
		}
		jobCommit := jobSpec.Refs.Pull[0].Commit

		if _, ok := (commitsMap)[jobCommit]; ok {
			ciLists = append(ciLists, fmt.Sprintf(jenkinsURL+"%d"+"/consoleFull", build.Number))
		}
	}

	return ciLists
}
