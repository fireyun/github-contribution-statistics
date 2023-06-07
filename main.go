package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"
)

type Commit struct {
	Title     string `json:"title"`
	URL       string `json:"html_url"`
	CreatedAt string `json:"created_at"`
}

type PullRequest struct {
	Title     string `json:"title"`
	URL       string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type Issue struct {
	Title     string `json:"title"`
	URL       string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type Statistics struct {
	PRsCount     int           `json:"prs_count"`
	PRStats      []PullRequest `json:"pr_stats"`
	IssuesCount  int           `json:"issues_count"`
	IssueStats   []Issue       `json:"issue_stats"`
	CommitsCount int           `json:"commits_count,omitempty"`
	CommitStats  []Commit      `json:"commit_stats,omitempty"`
}

func getContributorStatistics(repoOwner, repoName, contributorUsername, startDate, endDate string,
	includeCommits bool, authToken string) (Statistics, error) {
	baseURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", repoOwner, repoName)

	client := &http.Client{}

	var commitsData []Commit
	var commitsCount int

	if includeCommits {
		// Commits
		commitsURL := fmt.Sprintf("%s/commits?author=%s&since=%s&until=%s&per_page=100",
			baseURL, contributorUsername, startDate, endDate)

		// Create the HTTP request
		commitsReq, err := http.NewRequest("GET", commitsURL, nil)
		if err != nil {
			return Statistics{}, err
		}

		// Conditionally set the authentication token in the request header
		if authToken != "" {
			commitsReq.Header.Set("Authorization", "token "+authToken)
		}

		// Measure the time taken for the commits request
		startTime := time.Now()
		// Send the request
		commitsResp, err := client.Do(commitsReq)
		elapsedTime := time.Since(startTime)
		if err != nil {
			return Statistics{}, err
		}
		defer commitsResp.Body.Close()

		if err := decodeResponse(commitsResp, &commitsData); err != nil {
			return Statistics{}, err
		}
		commitsCount = len(commitsData)
		fmt.Printf("Commits request took %s\n", elapsedTime)
	}

	// Pull Requests
	prsURL := fmt.Sprintf("%s/pulls?state=all&since=%s&until=%s&creator=%s&per_page=100",
		baseURL, startDate, endDate, contributorUsername)
	// Measure the time taken for the PRs request
	startTime := time.Now()
	prsData, err := fetchAllPages(prsURL, authToken)
	elapsedTime := time.Since(startTime)
	if err != nil {
		return Statistics{}, err
	}

	// Filter PRs created by the contributor and within the desired date range
	var filteredPRs []PullRequest
	for _, pr := range prsData {
		if pr.User.Login == contributorUsername && isWithinDateRange(pr.CreatedAt, startDate, endDate) {
			filteredPRs = append(filteredPRs, pr)
		}
	}
	prsCount := len(filteredPRs)
	fmt.Printf("PRs request took %s\n", elapsedTime)

	// Issues
	issuesURL := fmt.Sprintf("%s/issues?state=all&since=%s&until=%s&creator=%s&per_page=100",
		baseURL, startDate, endDate, contributorUsername)
	// Measure the time taken for the issues request
	startTime = time.Now()
	issuesData, err := fetchAllPages(issuesURL, authToken)
	elapsedTime = time.Since(startTime)
	if err != nil {
		return Statistics{}, err
	}

	// Filter issues created by the contributor and within the desired date range
	var filteredIssues []Issue
	for _, issue := range issuesData {
		if issue.User.Login == contributorUsername && isWithinDateRange(issue.CreatedAt, startDate, endDate) {
			filteredIssues = append(filteredIssues, Issue(issue))
		}
	}
	issuesCount := len(filteredIssues)
	fmt.Printf("Issues request took %s\n", elapsedTime)

	// Create the statistics
	statistics := Statistics{
		PRsCount:    prsCount,
		PRStats:     filteredPRs,
		IssuesCount: issuesCount,
		IssueStats:  filteredIssues,
	}

	if includeCommits {
		statistics.CommitsCount = commitsCount
		statistics.CommitStats = commitsData
	}

	return statistics, nil
}

func isWithinDateRange(date, startDate, endDate string) bool {
	parsedDate, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return false
	}
	parsedStartDate, err := time.Parse(time.RFC3339, startDate)
	if err != nil {
		return false
	}
	parsedEndDate, err := time.Parse(time.RFC3339, endDate)
	if err != nil {
		return false
	}

	return parsedDate.After(parsedStartDate) && parsedDate.Before(parsedEndDate)
}

func fetchAllPages(url string, authToken string) ([]PullRequest, error) {
	var allData []PullRequest
	client := &http.Client{}

	for url != "" {
		// Create the HTTP request
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		// Conditionally set the authentication token in the request header
		if authToken != "" {
			req.Header.Set("Authorization", "token "+authToken)
		}

		// Send the request
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var data []PullRequest
		if err := decodeResponse(resp, &data); err != nil {
			return nil, err
		}

		allData = append(allData, data...)

		// Check if there is a next page
		linkHeader := resp.Header.Get("Link")
		nextURL := extractNextPageURL(linkHeader)
		url = nextURL

		time.Sleep(time.Millisecond * 10)
	}

	return allData, nil
}

func extractNextPageURL(linkHeader string) string {
	links := strings.Split(linkHeader, ",")
	for _, link := range links {
		components := strings.Split(strings.TrimSpace(link), ";")
		if len(components) == 2 && strings.TrimSpace(components[1]) == `rel="next"` {
			url := strings.Trim(components[0], "<>")
			return url
		}
	}
	return ""
}

func decodeResponse(resp *http.Response, target interface{}) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("response returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func generateHTML(statistics Statistics, filename string) error {
	tmpl := template.Must(template.ParseFiles("template.html"))

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, statistics)
}

func main() {
	// Get the current time and calculate the start and end dates for the most recent month
	currentTime := time.Now()
	startDate := currentTime.AddDate(0, -1, 0).Format(time.RFC3339)
	endDate := currentTime.Format(time.RFC3339)

	// Command line flags
	repoOwner := flag.String("repoOwner", "TencentBlueKing", "Repository owner")
	repoName := flag.String("repoName", "bk-bcs", "Repository name")
	contributorUsername := flag.String("contributorUsername", "fireyun", "Contributor username")
	startDateFlag := flag.String("startDate", startDate, "Start date (format: YYYY-MM-DD)")
	endDateFlag := flag.String("endDate", endDate, "End date (format: YYYY-MM-DD)")
	filename := flag.String("filename", "statistics.html", "Output filename")
	includeCommits := flag.Bool("includeCommits", false, "Include commit data in statistics")
	authToken := flag.String("authToken", "", "GitHub authentication token")
	flag.Parse()

	statistics, err := getContributorStatistics(*repoOwner, *repoName, *contributorUsername, *startDateFlag,
		*endDateFlag, *includeCommits, *authToken)
	if err != nil {
		log.Fatal(err)
	}

	err = generateHTML(statistics, *filename)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Statistics generated successfully. Please check the file: %s\n", *filename)
}
