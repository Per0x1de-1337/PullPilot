package diff

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	// Import your llm and models packages if calling directly here
	// "github.com/your-org/your-repo/pkg/llm"
	// "github.com/your-org/your-repo/pkg/models"
)

const (
	githubAPIURL    = "https://api.github.com"
	requestTimeout  = 30 * time.Second
	maxFilesPerPage = 100 // Max allowed by GitHub API for file listing
)

// Structs for parsing GitHub API JSON responses (minimal fields)
type GitHubPullRequest struct {
	Head struct {
		Sha string `json:"sha"` // Commit SHA of the PR's head branch
	} `json:"head"`
	// Add other PR details if needed
}

type GitHubFile struct {
	Filename string `json:"filename"` // Path relative to repo root
	Status   string `json:"status"`   // e.g., "added", "modified", "removed", "renamed"
	Patch    string `json:"patch"`    // Optional: patch content per file
	// We mainly need filename and status
}

type GitHubContent struct {
	Encoding string `json:"encoding"` // Usually "base64"
	Content  string `json:"content"`  // Base64 encoded content
	SHA      string `json:"sha"`      // Blob SHA
	Size     int    `json:"size"`     // File size in bytes
	// Add Type ("file", "dir", "symlink") if needed for filtering
}

// func getPullRequestDetails() (string, string, error) {
// 	PullRequestURL := os.Getenv("PULL_REQUEST_URL")
// 	owner, repoName, err := extractOwnerAndRepo(PullRequestURL)
// 	if err != nil {
// 		return "", "", fmt.Errorf("could not extract owner and repo from the URL: %w", err)
// 	}
// 	return owner, repoName, nil
// }

// ownerName1, repoNaame, err := extractOwnerAndRepo(PullRequest_url)

// getGitHubAPI performs a GET request to the GitHub API with appropriate headers.
func getGitHubAPI(ctx context.Context, apiURL string, token string, acceptHeader string) (*http.Response, error) {
	client := &http.Client{Timeout: requestTimeout}
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub API request for %s: %w", apiURL, err)
	}

	// Set Headers
	req.Header.Set("Authorization", "token "+token)
	if acceptHeader != "" {
		req.Header.Set("Accept", acceptHeader)
	} else {
		req.Header.Set("Accept", "application/vnd.github.v3+json") // Default to JSON
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28") // Recommended practice

	log.Printf("GitHub API Request: GET %s (Accept: %s)", apiURL, req.Header.Get("Accept"))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute GitHub API request for %s: %w", apiURL, err)
	}

	// Basic check for common client errors here (e.g., 4xx) before returning response
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body) // Read body for context even on error
		resp.Body.Close()                     // Close immediately after reading
		return nil, fmt.Errorf("GitHub API request failed for %s: Status %d %s, Body: %s", apiURL, resp.StatusCode, resp.Status, string(bodyBytes))
	}

	return resp, nil // Caller is responsible for closing resp.Body if status is OK
}

// GetDiffAndContentFromPR retrieves the diff and content of changed files for a specific GitHub Pull Request.
func GetDiffAndContentFromPR(ctx context.Context, owner, repo string, pullNumber int, token string) (string, map[string][]byte, error) {
	if owner == "" || repo == "" || pullNumber <= 0 || token == "" {
		return "", nil, fmt.Errorf("owner, repo, pullNumber, and token must be provided")
	}

	log.Printf("Getting diff and file contents for %s/%s PR #%d", owner, repo, pullNumber)

	// --- 1. Get the Pull Request Diff ---

	diffURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIURL, owner, repo, pullNumber)
	diffResp, err := getGitHubAPI(ctx, diffURL, token, "application/vnd.github.v3.diff")
	if err != nil {
		return "", nil, fmt.Errorf("failed to get PR diff: %w", err)
	}
	defer diffResp.Body.Close()

	diffBytes, err := io.ReadAll(diffResp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read PR diff response body: %w", err)
	}
	diffContent := string(diffBytes)
	log.Printf("Successfully fetched PR diff (length: %d chars)", len(diffContent))

	// --- 2. Get PR Metadata to find HEAD commit SHA ---
	// Needed to fetch file contents at the correct version
	prURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIURL, owner, repo, pullNumber)
	prInfoResp, err := getGitHubAPI(ctx, prURL, token, "application/vnd.github.v3+json")
	if err != nil {
		return "", nil, fmt.Errorf("failed to get PR metadata: %w", err)
	}
	defer prInfoResp.Body.Close() // Ensure this body is closed too

	var prData GitHubPullRequest
	if err := json.NewDecoder(prInfoResp.Body).Decode(&prData); err != nil {
		return "", nil, fmt.Errorf("failed to decode PR metadata JSON: %w", err)
	}
	if prData.Head.Sha == "" {
		return "", nil, fmt.Errorf("could not extract head commit SHA from PR metadata")
	}
	headSHA := prData.Head.Sha
	log.Printf("PR Head Commit SHA: %s", headSHA)

	// --- 3. List changed files in the PR ---
	var changedFiles []string // Store paths of non-deleted files
	page := 1
	for {
		filesURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=%d&page=%d",
			githubAPIURL, owner, repo, pullNumber, maxFilesPerPage, page)

		filesResp, err := getGitHubAPI(ctx, filesURL, token, "application/vnd.github.v3+json")
		if err != nil {
			// If listing files fails, we might still return the diff, but content will be incomplete
			log.Printf("Warning: Failed to list PR files (page %d): %v. File content map will be incomplete.", page, err)
			// Depending on requirements, you might return an error here instead
			break // Stop trying to list files
		}
		defer filesResp.Body.Close() // Ensure each page response body is closed

		var filesPage []GitHubFile
		if err := json.NewDecoder(filesResp.Body).Decode(&filesPage); err != nil {
			log.Printf("Warning: Failed to decode PR files list (page %d): %v. File content map may be incomplete.", page, err)
			// Return error or just stop fetching file list? Let's stop.
			break
		}

		if len(filesPage) == 0 {
			break // No more files on subsequent pages
		}

		log.Printf("Fetched page %d of changed files (%d files on page)", page, len(filesPage))
		for _, file := range filesPage {
			// We need content for added or modified files. Skip deleted files.
			if file.Status != "removed" {
				changedFiles = append(changedFiles, file.Filename)
			} else {
				log.Printf("  - Skipping removed file: %s", file.Filename)
			}
		}

		// Check Link header for next page (basic pagination)
		linkHeader := filesResp.Header.Get("Link")
		if !strings.Contains(linkHeader, `rel="next"`) {
			break // No next page indication
		}
		page++
	}
	log.Printf("Found %d changed (added/modified) files requiring content fetching.", len(changedFiles))

	// --- 4. Fetch content for each changed file ---
	changedFilesContent := make(map[string][]byte)
	for _, filePath := range changedFiles {
		// URL encode the file path to handle spaces, special chars, etc.
		encodedPath := url.PathEscape(filePath)
		contentURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
			githubAPIURL, owner, repo, encodedPath, headSHA)

		// Add a small delay if needed to avoid secondary rate limits, although usually not required for moderate PRs.
		// time.Sleep(50 * time.Millisecond)

		contentResp, err := getGitHubAPI(ctx, contentURL, token, "application/vnd.github.v3+json")
		if err != nil {
			log.Printf("Warning: Failed to get content for file '%s' at ref '%s': %v. Skipping content.", filePath, headSHA, err)
			// Check for 404 specifically - might indicate submodule or non-file?
			// if strings.Contains(err.Error(), "Status 404") { ... }
			continue // Skip content for this file
		}
		defer contentResp.Body.Close() // Important inside the loop!

		var contentData GitHubContent
		if err := json.NewDecoder(contentResp.Body).Decode(&contentData); err != nil {
			log.Printf("Warning: Failed to decode content JSON for file '%s': %v. Skipping content.", filePath, err)
			continue // Skip content for this file
		}

		// Check encoding and decode content
		if contentData.Encoding != "base64" {
			// GitHub API v3 generally returns base64 for file content.
			// If it's something else (e.g., "none" for symlinks or large files directing to blobs api), handle appropriately.
			log.Printf("Warning: Unexpected content encoding '%s' for file '%s' (size %d). Skipping content.", contentData.Encoding, filePath, contentData.Size)
			// Note: Files > 1MB might have encoding "none" and require the Git Blobs API (`/repos/{owner}/{repo}/git/blobs/{blob_sha}`).
			// This implementation does *not* handle files > 1MB via the blobs API.
			continue
		}

		decodedContent, err := base64.StdEncoding.DecodeString(contentData.Content)
		if err != nil {
			log.Printf("Warning: Failed to decode base64 content for file '%s': %v. Skipping content.", filePath, err)
			continue // Skip content for this file
		}

		log.Printf("  - Fetched content for %s (decoded size: %d bytes)", filePath, len(decodedContent))
		changedFilesContent[filePath] = decodedContent
	}

	log.Printf("Finished fetching content for %d files.", len(changedFilesContent))
	return diffContent, changedFilesContent, nil
}

// --- Example Usage ---
func extractPullNumber(PullRequest_url string) string {
	if PullRequest_url == "" {
		return ""
	}

	parts := strings.Split(PullRequest_url, "/")
	if len(parts) < 2 {
		return ""
	}

	return parts[len(parts)-1]
}
func extractOwnerAndRepo(PullRequest_url string) (string, string, error) {
	if PullRequest_url == "" {
		return "", "", errors.New("PullRequest_url is empty")
	}

	parts := strings.Split(PullRequest_url, "/")
	if len(parts) < 5 {
		return "", "", errors.New("invalid PullRequest_url format")
	}

	owner := parts[len(parts)-4]
	repo := parts[len(parts)-3]
	return owner, repo, nil
}
func main() {
	// --- Get configuration from environment variables or flags ---
	PullRequest_url := os.Getenv("PULL_REQUEST_URL")

	repoOwner, repoName, err := extractOwnerAndRepo(PullRequest_url)
	if err != nil {
		log.Fatalf("could not extract owner and repo from the URL: %v", err)
	}
	pull_number := extractPullNumber(PullRequest_url)
	if pull_number == "" {
		log.Fatal("could not extract pull number from the URL")
	}

	pullRequestNumber, err := strconv.Atoi(pull_number)
	if err != nil {
		log.Fatalf("failed to convert pull number to integer: %v", err)
	}
	githubToken := os.Getenv("GITHUB_TOKEN") // REQUIRED
	// repoOwner := os.Getenv("GITHUB_REPOSITORY_OWNER") // e.g., "octocat"
	// repoName := "" // Needs to be derived from GITHUB_REPOSITORY
	// pullRequestNumberStr := os.Getenv("GITHUB_PULL_REQUEST_NUMBER") // Often set in CI like GitHub Actions

	if githubToken == "" {
		log.Fatal("GITHUB_TOKEN environment variable is not set.")
	}

	if repoOwner == "" || repoName == "" {
		log.Fatal("Could not determine repository owner/name (set GITHUB_REPOSITORY_OWNER/GITHUB_REPOSITORY or ensure GITHUB_REPOSITORY is 'owner/repo').")
	}

	fmt.Printf("--- Analyzing PR %s/%s #%d ---\n", repoOwner, repoName, pullRequestNumber)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // Longer timeout for API calls
	defer cancel()

	diffContent, changedFilesContent, err := GetDiffAndContentFromPR(ctx, repoOwner, repoName, pullRequestNumber, githubToken)
	fmt.Printf("Diff content: %s\n", diffContent)
	fmt.Printf("Changed files content: %v\n", changedFilesContent)
	if err != nil {
		log.Fatalf("Failed to get diff and content from GitHub API: %v", err)
	}

	// Check if diff is empty (e.g., empty commit in PR?)
	if diffContent == "" && len(changedFilesContent) == 0 {
		log.Println("Warning: Diff content and changed files map are both empty. No changes found or error fetching.")
		// Decide whether to proceed or exit
		// return
	}

	log.Printf("GitHub API Result: Diff length: %d, Files with content: %d\n", len(diffContent), len(changedFilesContent))

	// --- Pass to your LLM Analyzer ---
	/*
			aiCfg := &llm.AIConfig{
				MaxOutputTokens: 1024,
				Temperature:   0.2,
				MinSeverity:   models.SeverityWarning,
		        // ModelName: "gemini-1.5-flash-latest", // Optional: configure model
			}
		    apiKey := os.Getenv("YOUR_LLM_API_KEY") // Replace with your LLM key env var
			client := llm.NewGoogleAIClient(apiKey, aiCfg)


			issues, err := client.AnalyzeCode(ctx, diffContent, changedFilesContent) // Pass fetched data
			if err != nil {
				log.Fatalf("AI analysis failed: %v", err)
			}

			// Process issues...
		    log.Printf("AI Analysis found %d issues:", len(issues))
		    for _, issue := range issues {
		        fmt.Printf("- [%s] %s:%d Severity: %s\n  %s\n",
		             issue.Source, issue.Path, issue.Line, issue.Severity, issue.Title)
		    }
	*/
}
