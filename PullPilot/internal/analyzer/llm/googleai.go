package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/keploy/PullPilot/pkg/models"
)

const (
	googleAIEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"
	maxRetries       = 3
	baseDelay        = 1 * time.Second
)

var jsonRegex = regexp.MustCompile(`(?s)\[\s*{.*?}\s*\]`)

type GoogleAIClient struct {
	apiKey     string
	httpClient *http.Client
	config     *AIConfig
}

type AIConfig struct {
	MaxTokens   int
	Temperature float64
	MinSeverity models.Severity
}

func NewGoogleAIClient(apiKey string, cfg *AIConfig) *GoogleAIClient {

	return &GoogleAIClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config: cfg,
	}
}

func (g *GoogleAIClient) AnalyzeCode(ctx context.Context, files []*models.File) ([]*models.Issue, error) {

	var allIssues []*models.Issue

	for _, file := range files {

		if shouldSkipFile(file.Path) {
			fmt.Println("Skipping file:", file.Path)
			continue
		}

		issues, err := g.analyzeFile(ctx, file)
		if err != nil {
			log.Printf("AI analysis failed for %s: %v", file.Path, err)
			continue
		}

		allIssues = append(allIssues, filterIssues(issues, g.config.MinSeverity)...)
		fmt.Println("Filtered issues for", file.Path, ":", allIssues)
	}

	fmt.Println("AnalyzeCode: Completed analysis with", len(allIssues), "total issues")
	return allIssues, nil
}

func shouldSkipFile(path string) bool {
	ext := filepath.Ext(path)
	skip := !(ext == ".go" || ext == ".js" || ext == ".ts" || ext == ".py")

	return skip
}

func (g *GoogleAIClient) analyzeFile(ctx context.Context, file *models.File) ([]*models.Issue, error) {
	fmt.Println("Analyzing file:", file.Path)
	prompt := buildPrompt(file.Content)

	var response string
	var err error

	for i := 0; i < maxRetries; i++ {
		fmt.Println("Attempt", i+1, "to call generateContent")
		response, err = g.generateContent(ctx, prompt)
		if err == nil {
			break
		}
		fmt.Println("Retrying in", baseDelay*time.Duration(i*i), "due to error:", err)
		time.Sleep(baseDelay * time.Duration(i*i))
	}
	if err != nil {
		fmt.Println("Failed to analyze file after retries:", err)
		return nil, err
	}

	return parseAIResponse(response, file.Path)
}

func buildPrompt(code string) string {
	prompt := fmt.Sprintf(`Analyze this code for security, performance, and maintainability issues.

Code:
%s

Respond in JSON format:
[{
	"line": <number>,
	"category": "security|performance|maintainability|error_handling",
	"description": "<concise issue description>",
	"severity": "high|medium|low",
	"suggestion": "<specific improvement suggestion>",
	"confidence": 0-1
}]

Rules:
1. Only report issues with confidence >= 0.7
2. Line numbers must be accurate
3. Suggest concrete fixes
4. Avoid trivial/style-only issues`, code)

	return prompt
}

func (g *GoogleAIClient) generateContent(ctx context.Context, prompt string) (string, error) {
	fmt.Println("Generating content with AI for prompt of length:", len(prompt))

	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     g.config.Temperature,
			"maxOutputTokens": g.config.MaxTokens,
		},
		"safetySettings": []map[string]interface{}{
			{
				"category":  "HARM_CATEGORY_DANGEROUS_CONTENT",
				"threshold": "BLOCK_ONLY_HIGH",
			},
		},
	}

	jsonBody, _ := json.Marshal(requestBody)

	req, _ := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s?key=%s", googleAIEndpoint, g.apiKey),
		bytes.NewBuffer(jsonBody),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		fmt.Println("API request failed:", err)
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := readBody(resp)
		fmt.Println("API Error:", resp.StatusCode, body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, body)
	}

	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		fmt.Println("Failed to decode API response:", err)
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Candidates) == 0 {
		fmt.Println("No content in API response")
		return "", fmt.Errorf("no content in response")
	}

	return response.Candidates[0].Content.Parts[0].Text, nil
}

func parseAIResponse(response, filePath string) ([]*models.Issue, error) {

	jsonStr := jsonRegex.FindString(response)
	if jsonStr == "" {
		fmt.Println("No JSON found in response")
		return nil, fmt.Errorf("no JSON found in response")
	}

	if !strings.HasSuffix(strings.TrimSpace(jsonStr), "]") {
		fmt.Println("Detected incomplete JSON, attempting to fix...")
		jsonStr += "]" // Close the JSON array (basic fix)
	}

	var rawIssues []struct {
		Line        int     `json:"line"`
		Category    string  `json:"category"`
		Description string  `json:"description"`
		Severity    string  `json:"severity"`
		Suggestion  string  `json:"suggestion"`
		Confidence  float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawIssues); err != nil {
		fmt.Println("Invalid JSON format:", err)
		return nil, fmt.Errorf("invalid JSON format: %w", err)
	}

	var issues []*models.Issue

	for _, ri := range rawIssues {
		if ri.Confidence < 0.7 {
			continue
		}

		issues = append(issues, &models.Issue{
			Path:        filePath,
			Line:        ri.Line,
			Title:       fmt.Sprintf("[%s] %s", strings.ToUpper(ri.Category), ri.Description),
			Description: ri.Description,
			Severity:    mapSeverity(ri.Severity),
			Suggestion:  ri.Suggestion,
			Source:      "AI Analysis",
		})
	}

	return issues, nil
}

func mapSeverity(s string) models.Severity {
	switch strings.ToLower(s) {
	case "high":
		return models.SeverityError
	case "medium":
		return models.SeverityWarning
	default:
		return models.SeverityInfo
	}
}

func filterIssues(issues []*models.Issue, min models.Severity) []*models.Issue {
	var filtered []*models.Issue
	for _, issue := range issues {
		if issue.Severity >= min {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

func readBody(resp *http.Response) string {
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}
