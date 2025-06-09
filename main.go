package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gomarkdown/markdown"      // Import gomarkdown
	"github.com/gomarkdown/markdown/html" // Import HTML renderer
)

const (
	geminiAPIEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=" // Gemini API endpoint
	defaultPort       = "8055"                                                                                              // Default port for the web server
	defaultWebhookURL = ""                                                                                                // default Webhook Url
	defaultSystemPrompt = "You are a helpful code review assistant. You will receive a pull request context containing the file contents, the user question and you are supposed to give accurate, concise answers. Format your response in markdown where appropriate, especially use it for code snippets." // Default System Prompt
	shutdownTimeout = 5 * time.Second // Maximum time to wait for shutdown
)

var (
	indexTemplate = template.Must(template.New("index").ParseFiles("index.html")) 
)

type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback struct {
		SafetyRatings []struct {
			Category    string `json:"category"`
			Probability string `json:"probability"`
		} `json:"safetyRatings"`
	} `json:"promptFeedback"`
}

type ChatMessage struct {
	Sender string `json:"sender"` // "user" or "gemini"
	Text   string `json:"text"`
	IsHTML bool   `json:"isHTML"` // Indicates if the Text field contains HTML
}

// Global variables to store PR context and chat history.  Not ideal for production, but simplifies the example.
var (
	prContext   string
	chatHistory []ChatMessage
	geminiKey   string // Global for Gemini API key
	githubToken string // Global for GitHub token
	webhookURL  string // Global for webhook URL
	systemPrompt string // Global for the system prompt
	isStopped   bool      // Global flag to indicate if the process has been stopped
	stopChan    chan bool // Channel to signal the main loop to stop
)

func main() {
	// Define command-line flags
	var (
		apiKeyFlag      = flag.String("gemini-key", "", "Gemini API key (required)")
		githubTokenFlag = flag.String("github-token", "", "GitHub token (required)")
		prURLFlag       = flag.String("pr-url", "", "GitHub PR URL (required)")
		webhookURLFlag  = flag.String("webhook-url", defaultWebhookURL, "Webhook URL to call on stop (optional)")
		systemPromptFlag = flag.String("system-prompt", defaultSystemPrompt, "YOU are an advanced code review bot. SO according to the user query respond to it according to all the information you have") // allow to change system prompt
	)

	// Parse command-line flags
	flag.Parse()

	// Validate required flags
	if *apiKeyFlag == "" || *githubTokenFlag == "" || *prURLFlag == "" {
		flag.Usage()
		log.Fatal("Error: --gemini-key, --github-token, and --pr-url are required")
	}

	// Assign flag values to global variables
	geminiKey = *apiKeyFlag
	githubToken = *githubTokenFlag
	prURL := *prURLFlag
	webhookURL = *webhookURLFlag
	systemPrompt = *systemPromptFlag // Store the system prompt

	stopChan = make(chan bool, 1)

	// Handle OS signals for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM) // Listen for Ctrl+C and SIGTERM

	// Initialize PR context immediately on startup.  This avoids needing the UI to kick off the process.
	err := initializePRContext(prURL)
	if err != nil {
		log.Fatalf("Error initializing PR context: %v", err)
	}

	// Serve static files (CSS, JS, etc.)
	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	http.HandleFunc("/chat", chatHandler(geminiKey))
	http.HandleFunc("/stop", stopHandler()) // Add the stop handler

	port := os.Getenv("PORT") // Heroku provides the port via environment variable.
	if port == "" {
		port = defaultPort
	}

	server := &http.Server{
		Addr: ":" + port,
		// Optionally configure timeouts:
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// Run server in goroutine to allow for graceful shutdown
	go func() {
		fmt.Printf("Server listening on port %s\n", port)
		// Proper error handling for listen: address already in use
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			log.Fatalf("Error listening: %v", err)
		}
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v", err)
		}
	}()

	// Wait for stop signal or OS signal
	select {
	case <-stopChan:
		fmt.Println("Stop signal received.")
	case sig := <-signalChan:
		fmt.Printf("OS signal received: %v\n", sig)
	}

	fmt.Println("Shutting down server...")

	// Create a context for graceful shutdown with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Calling the webhook (moved here to ensure it's called before exit)
	if webhookURL != "" {
		fmt.Printf("Calling webhook: %s\n", webhookURL)
		err := callWebhook(webhookURL)
		if err != nil {
			log.Printf("Error calling webhook: %v", err)
		} else {
			fmt.Println("Webhook called successfully.")
		}
	}

	// Shutdown the HTTP server gracefully
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	fmt.Println("Exiting.")
	os.Exit(0) // Exit the program
}

// initializePRContext fetches and stores the PR context based on the given PR URL.
func initializePRContext(prURL string) error {
	// Extract repository and PR number from the URL.
	parts := strings.Split(prURL, "/")
	if len(parts) < 5 {
		return fmt.Errorf("invalid PR URL format. Expected: https://github.com/owner/repo/pull/number")
	}
	owner := parts[3]
	repo := parts[4]
	prNumber := parts[6]

	// Get the list of changed files in the PR
	changedFiles, err := getPRChangedFiles(owner, repo, prNumber, githubToken)
	if err != nil {
		return fmt.Errorf("failed to get PR changed files: %v", err)
	}

	// Load the content of each changed file
	fileContents := make(map[string]string)
	for _, file := range changedFiles {
		content, err := getFileContentFromPR(owner, repo, prNumber, file, githubToken)
		if err != nil {
			log.Printf("Failed to get content for file %s: %v", file, err)
			continue // Skip to the next file if one fails
		}
		fileContents[file] = content
	}

	// Construct the initial context for the LLM. This includes the diff and the full file contents.
	prContext = buildInitialContext(fileContents) // Store in global variable

	// Reset chat history.
	chatHistory = []ChatMessage{}
	return nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	err := indexTemplate.Execute(w, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func chatHandler(apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if isStopped {
			respondWithError(w, "Chat is stopped.")
			return
		}

		var requestBody struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		question := requestBody.Question
		if question == "" {
			http.Error(w, "Question is required", http.StatusBadRequest)
			return
		}

		// Add user message to chat history.
		chatHistory = append(chatHistory, ChatMessage{Sender: "user", Text: question, IsHTML: false})

		// Construct the complete prompt for the LLM, including the system prompt.
		prompt := fmt.Sprintf("%s\n\nHere is the pull request context:\n%s\n\nUser question: %s", systemPrompt, prContext, question)

		response, err := queryGemini(apiKey, prompt)
		if err != nil {
			log.Printf("Error querying Gemini API: %v", err)
			respondWithError(w, "Error querying Gemini API")
			return
		}

		// Convert Gemini's Markdown response to HTML
		htmlOutput := renderMarkdown(response)

		// Add Gemini's response to chat history
		chatHistory = append(chatHistory, ChatMessage{Sender: "gemini", Text: htmlOutput, IsHTML: true})

		respondWithJSON(w, map[string]interface{}{
			"message":     "Response received",
			"chatHistory": chatHistory,
		})
	}
}

// stopHandler handles the stop request.
func stopHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		isStopped = true
		fmt.Println("Stop signal received, initiating shutdown...")

		// Send signal to stop main loop
		stopChan <- true

		respondWithJSON(w, map[string]interface{}{"message": "Chat stopped, server is shutting down"})
	}
}

// callWebhook makes a POST request to the specified URL.
func callWebhook(url string) error {
	_, err := http.Post(url, "application/json", nil) // Basic POST request
	return err
}

// renderMarkdown converts Markdown text to HTML.
func renderMarkdown(md string) string {
	// Set up options for Markdown rendering
	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	markdownBytes := []byte(md)
	htmlBytes := markdown.ToHTML(markdownBytes, nil, renderer)
	return string(htmlBytes)
}

// getPRChangedFiles gets the list of changed files in a pull request using the GitHub CLI.
func getPRChangedFiles(owner, repo, prNumber string, githubToken string) ([]string, error) {
	cmd := exec.Command("gh", "pr", "view", prNumber, "--repo", fmt.Sprintf("%s/%s", owner, repo), "--json", "files", "-q", ".files.[].path")
	cmd.Env = append(os.Environ(), fmt.Sprintf("GITHUB_TOKEN=%s", githubToken)) // Set the GITHUB_TOKEN environment variable
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("error executing gh pr view for files: %v, stderr: %s", err, stderr.String())
	}

	// The output is a string with newline-separated file paths.
	files := strings.Split(strings.TrimSpace(out.String()), "\n")
	return files, nil
}

// getFileContentFromPR gets the content of a specific file from a pull request.
func getFileContentFromPR(owner, repo, prNumber, filePath string, githubToken string) (string, error) {
	// Use `gh api` to get the file content directly from the GitHub API.  This avoids needing to checkout the PR locally.
	// The GitHub API endpoint is:  /repos/{owner}/{repo}/contents/%s?ref=refs/pull/%s/head
	apiURL := fmt.Sprintf("repos/%s/%s/contents/%s?ref=refs/pull/%s/head", owner, repo, filePath, prNumber)

	cmd := exec.Command("gh", "api", apiURL)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GITHUB_TOKEN=%s", githubToken)) // Set the GITHUB_TOKEN environment variable
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error executing gh api for file content: %v, stderr: %s", err, stderr.String())
	}

	var apiResponse map[string]interface{}
	err = json.Unmarshal(out.Bytes(), &apiResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshaling API response: %v", err)
	}

	// The content is base64 encoded in the "content" field.
	contentBase64, ok := apiResponse["content"].(string)
	if !ok {
		return "", fmt.Errorf("content field not found in API response or is not a string")
	}

	// Decode the base64 content.
	contentBytes, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(contentBase64)))
	if err != nil {
		return "", fmt.Errorf("error decoding base64 content: %v", err)
	}

	return string(contentBytes), nil
}

// buildInitialContext constructs the initial context for the LLM, including all file contents.
func buildInitialContext(fileContents map[string]string) string {
	var context strings.Builder
	context.WriteString("Here are the contents of the files changed in this pull request:\n\n")

	for file, content := range fileContents {
		context.WriteString(fmt.Sprintf("--- File: %s ---\n", file))
		context.WriteString(content)
		context.WriteString("\n\n")
	}

	return context.String()
}

// queryGemini sends a request to the Gemini API and returns the response.
func queryGemini(apiKey string, prompt string) (string, error) {
	url := geminiAPIEndpoint + apiKey

	requestData := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{
						Text: prompt,
					},
				},
			},
		},
	}

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %v", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("error making request to Gemini API: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API returned status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var geminiResponse GeminiResponse
	err = json.Unmarshal(body, &geminiResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshaling response body: %v", err)
	}

	if len(geminiResponse.Candidates) > 0 && len(geminiResponse.Candidates[0].Content.Parts) > 0 {
		return geminiResponse.Candidates[0].Content.Parts[0].Text, nil
	}

	return "No response from Gemini API.", nil
}

func respondWithError(w http.ResponseWriter, message string) {
	respondWithJSON(w, map[string]interface{}{"error": message})
}

func respondWithJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}