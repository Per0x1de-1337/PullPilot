package github

import (
	// "bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"os"

	"github.com/keploy/PullPilot/internal/shared"
	"github.com/keploy/PullPilot/pkg/models"
)

var pullnumber int

func PullRequestNumber(currentpullnumber int) int {
	pullnumber = currentpullnumber
	return pullnumber
}

var comment string

type Client struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: "https://api.github.com",
	}
}

type File struct {
	Path    string
	Content []byte
}

func (c *Client) GetChangedFiles(ctx context.Context, owner, repo string, pullNumber int) ([]*models.File, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files", c.baseURL, owner, repo, pullNumber)
	fmt.Printf("Fetching PR files from: %s\n", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	fmt.Printf("Base64 encoded token: %s\n", base64.StdEncoding.EncodeToString([]byte(c.token)))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %s, response: %s", resp.Status, string(body))
	}

	var prFiles []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
		RawURL   string `json:"raw_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&prFiles); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var files []*models.File
	for _, prFile := range prFiles {
		if prFile.Status == "removed" {
			continue // Skip deleted files
		}

		content, err := fetchRawContent(ctx, prFile.RawURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch content for %s: %w", prFile.Filename, err)
		}

		files = append(files, &models.File{
			Path:    prFile.Filename,
			Content: content,
		})
	}

	return files, nil
}

func fetchRawContent(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for raw content: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch raw content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub raw file error: %s, response: %s", resp.Status, string(body))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read raw content: %w", err)
	}

	return string(data), nil
}

// func (c *Client) CreateReview(ctx context.Context, owner, repo string, pullnumber int, comments []*models.ReviewComment) error {
// 	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.baseURL, owner, repo, pullnumber)

// 	for _, comment := range comments {
// 		fmt.Printf("comments are ..,.,.,  : %v\n", comment)
// 	}
// 	fmt.Printf("URL is: %s\n", url)

// 	var markdownComment string
// 	markdownComment += "### 📝 Automated Review Comments\n\n"
// 	markdownComment += "Thank you for raising this pull request. Below are the review comments:\n\n"

// 	for _, comment := range comments {
// 		markdownComment += fmt.Sprintf(
// 			"- **File:** %s\n  - **Position:** %d\n  - **Comment:** %s\n\n",
// 			comment.Path, comment.Position, comment.Body,
// 		)
// 	}

// 	payload := map[string]interface{}{
// 		"body": markdownComment,
// 	}

// 	jsonPayload, err := json.Marshal(payload)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal review payload: %w", err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
// 	if err != nil {
// 		return fmt.Errorf("failed to create request: %w", err)
// 	}

// 	req.Header.Set("Authorization", "token "+c.token)
// 	req.Header.Set("Accept", "application/vnd.github.v3+json")
// 	req.Header.Set("Content-Type", "application/json")

// 	resp, err := c.httpClient.Do(req)
// 	if err != nil {
// 		return fmt.Errorf("failed to send request: %w", err)
// 	}
// 	defer resp.Body.Close()

// 	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
// 		body, _ := ioutil.ReadAll(resp.Body)
// 		log.Printf("GitHub API Response: %s", string(body))
// 		return fmt.Errorf("GitHub API error: %s, %s", resp.Status, string(body))
// 	}

// 	log.Println("Review successfully posted to GitHub.")
// 	return nil
// }

func (c *Client) CreateReview(ctx context.Context, owner, repo string, pullnumber int, comments []*models.ReviewComment) error {
	// Format the comment
	var markdownComment string
	markdownComment += "### 📝 Automated Review Comments\n\n"
	markdownComment += "Thank you for raising this pull request. Below are the review comments:\n\n"

	for _, comment := range comments {
		markdownComment += fmt.Sprintf(
			"- **File:** %s\n  - **Position:** %d\n  - **Comment:** %s\n\n",
			comment.Path, comment.Line, comment.Body,
		)
	}

	// Generate filename using current timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("Before_review_%s.md", timestamp)

	// Write to file
	err := os.WriteFile(filename, []byte(markdownComment), 0644)
	if err != nil {
		return fmt.Errorf("failed to write review to file: %w", err)
	}

	log.Printf("*********************************************************************************************Review comments saved to file: %s\n", filename)
	return nil
}
func base64Decode(content string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("base64 decode error: %w", err)
	}
	return decoded, nil
}

func (c *Client) ProcessPullRequestReview(ctx context.Context, owner, repo string, pullnumber int) error {

	changedFiles, err := c.GetChangedFiles(ctx, owner, repo, pullnumber)
	if err != nil {
		return fmt.Errorf("failed to get changed files: %w", err)
	}
	fmt.Println("changedFiles: track01 ", changedFiles)
	ghoda := shared.GetAllIssues()
	fmt.Println("ghoda: track01 ", ghoda)

	var reviewComments []*models.ReviewComment
	for _, file := range changedFiles {

		reviewComments = append(reviewComments, &models.ReviewComment{
			Path:     file.Path,
			Position: 1, // Assuming position 1 for the sake of this example
			Body:     "Please review this file.",
		})
	}

	return nil
}
