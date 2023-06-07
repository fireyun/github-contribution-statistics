package main

import (
	"embed"
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

//go:embed templates/*
var templates embed.FS

type SearchResult struct {
	Items []Content `json:"items"`
}

type Statistics struct {
	PRsCount     int       `json:"prs_count"`
	PRStats      []Content `json:"pr_stats"`
	IssuesCount  int       `json:"issues_count"`
	IssueStats   []Content `json:"issue_stats"`
	CommitsCount int       `json:"commits_count,omitempty"`
	CommitStats  []Content `json:"commit_stats,omitempty"`
}

type Content struct {
	Title     string `json:"title"`
	URL       string `json:"html_url"`
	CreatedAt string `json:"created_at"`
}

// API doc: https://docs.github.com/en/rest/search?apiVersion=2022-11-28#search-issues-and-pull-requests
func getContributorStatistics(repoOwner, repoName, contributorUsername, startDate, endDate string,
	includeCommits bool, authToken string, debug bool) (Statistics, error) {
	baseURL := "https://api.github.com/search/issues"

	client := &http.Client{}

	var commitsData []Content
	var commitsCount int

	if includeCommits {
		// Commits
		commitsURL := fmt.Sprintf("%s?q=repo:%s/%s+type:commit+author:%s+created:%s..%s",
			baseURL, repoOwner, repoName, contributorUsername, startDate, endDate)

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
		if debug {
			fmt.Printf("Commit HTTP Request URL: %s\n", commitsURL)
		}
		commitsResp, err := client.Do(commitsReq)
		elapsedTime := time.Since(startTime)
		if err != nil {
			return Statistics{}, err
		}
		defer commitsResp.Body.Close()

		var searchResult SearchResult
		if err := decodeResponse(commitsResp, &searchResult); err != nil {
			return Statistics{}, err
		}

		commitsData = searchResult.Items
		commitsCount = len(commitsData)
		fmt.Printf("Commits request took %s\n", elapsedTime)
	}

	// Pull Requests
	prsURL := fmt.Sprintf("%s?q=repo:%s/%s+type:pr+author:%s+created:%s..%s",
		baseURL, repoOwner, repoName, contributorUsername, startDate, endDate)
	// Measure the time taken for the PRs request
	startTime := time.Now()
	if debug {
		fmt.Printf("PR HTTP Request URL: %s\n", prsURL)
	}
	prsData, err := fetchAllPages(prsURL, authToken, debug)
	elapsedTime := time.Since(startTime)
	if err != nil {
		return Statistics{}, err
	}

	prsCount := len(prsData)
	fmt.Printf("PRs request took %s\n", elapsedTime)

	// Issues
	issuesURL := fmt.Sprintf("%s?q=repo:%s/%s+type:issue+author:%s+created:%s..%s",
		baseURL, repoOwner, repoName, contributorUsername, startDate, endDate)
	// Measure the time taken for the issues request
	startTime = time.Now()
	if debug {
		fmt.Printf("Issue HTTP Request URL: %s\n", issuesURL)
	}
	issuesData, err := fetchAllPages(issuesURL, authToken, debug)
	elapsedTime = time.Since(startTime)
	if err != nil {
		return Statistics{}, err
	}

	issuesCount := len(issuesData)
	fmt.Printf("Issues request took %s\n", elapsedTime)

	// Create the statistics
	statistics := Statistics{
		PRsCount:    prsCount,
		PRStats:     prsData,
		IssuesCount: issuesCount,
		IssueStats:  issuesData,
	}

	if includeCommits {
		statistics.CommitsCount = commitsCount
		statistics.CommitStats = commitsData
	}

	return statistics, nil
}

func fetchAllPages(url string, authToken string, debug bool) ([]Content, error) {
	var allData []Content
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

		var searchResult SearchResult
		if err := decodeResponse(resp, &searchResult); err != nil {
			return nil, err
		}

		allData = append(allData, searchResult.Items...)

		// Check if there is a next page
		linkHeader := resp.Header.Get("Link")
		nextURL := extractNextPageURL(linkHeader)
		url = nextURL
		if debug {
			fmt.Printf("next HTTP Request URL: %s\n", url)
		}
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
	tmpl, err := template.ParseFS(templates, "templates/template.html")
	if err != nil {
		return err
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, statistics)
}

func validateTime(startTime, endTime string) {
	// Parse the start and end dates from the command line flags
	sDate, err := time.Parse("2006-01-02", startTime)
	if err != nil {
		log.Fatalf("Invalid start date format: %s", err)
	}
	eDate, err := time.Parse("2006-01-02", endTime)
	if err != nil {
		log.Fatalf("Invalid end date format: %s", err)
	}

	// Ensure that the end date is after the start date
	if eDate.Before(sDate) {
		log.Fatal("End date must be after start date")
	}
}

func main() {
	// Get the current time and calculate the start and end dates for the most recent month
	currentTime := time.Now()
	startDate := currentTime.AddDate(0, -1, 0).Format("2006-01-02")
	endDate := currentTime.Format("2006-01-02")

	// Command line flags
	repoOwner := flag.String("repoOwner", "TencentBlueKing", "Repository owner")
	repoName := flag.String("repoName", "bk-bcs", "Repository name")
	contributorUsername := flag.String("contributorUsername", "fireyun", "Contributor username")
	startDateFlag := flag.String("startDate", startDate, "Start date (format: YYYY-MM-DD)")
	endDateFlag := flag.String("endDate", endDate, "End date (format: YYYY-MM-DD)")
	filename := flag.String("filename", "statistics.html", "Output filename")
	includeCommits := flag.Bool("includeCommits", false, "Include commit data in statistics")
	authToken := flag.String("authToken", "", "GitHub authentication token")
	debug := flag.Bool("debug", true, "Enable debug mode to print HTTP request URLs")

	flag.Parse()

	validateTime(*startDateFlag, *endDateFlag)
	if *debug {
		fmt.Println("Debug mode is enabled")
		flag.VisitAll(func(f *flag.Flag) {
			fmt.Printf("flag -%s=%s\n", f.Name, f.Value)
		})
	}

	statistics, err := getContributorStatistics(*repoOwner, *repoName, *contributorUsername, *startDateFlag,
		*endDateFlag, *includeCommits, *authToken, *debug)
	if err != nil {
		log.Fatal(err)
	}

	err = generateHTML(statistics, *filename)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Statistics generated successfully. Please check the file: %s\n", *filename)
}
