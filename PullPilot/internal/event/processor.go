package event

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/keploy/PullPilot/internal/analyzer"
	"github.com/keploy/PullPilot/internal/config"
)

type Processor struct {
	cfg          *config.Config
	orchestrator *analyzer.Orchestrator
}

func NewProcessor(cfg *config.Config) *Processor {
	return &Processor{
		cfg:          cfg,
		orchestrator: analyzer.NewOrchestrator(cfg),
	}
}

func (p *Processor) ProcessGitHubEvent(eventType string, payload []byte) error {

	PullRequest_url := os.Getenv("PULL_REQUEST_URL")

	owner, repoName, err := extractOwnerAndRepo(PullRequest_url)
	if err != nil {
		return fmt.Errorf("could not extract owner and repo from the URL: %w", err)
	}
	pull_number := extractPullNumber(PullRequest_url)
	if pull_number == "" {
		return fmt.Errorf("could not extract pull number from the URL")
	}

	prNumber, err := strconv.Atoi(pull_number)
	if err != nil {
		return fmt.Errorf("failed to convert pull number to integer: %w", err)
	}
	fmt.Printf("all things are good\n")
	fmt.Printf("PullRequest_url: %s\n", PullRequest_url)
	fmt.Printf("owner: %s\n", owner)
	fmt.Printf("repoName: %s\n", repoName)
	fmt.Printf("pull_number: %d\n", prNumber)
	job := &analyzer.Job{
		Provider:  "github",
		RepoOwner: owner,
		RepoName:  repoName,
		PRNumber:  prNumber,
	}

	log.Printf("Starting analysis for %s/%s PR ", job.RepoOwner, job.RepoName)
	if issues, err := p.orchestrator.AnalyzeCode(job); err != nil {
		return fmt.Errorf("failed to analyze code: %w", err)
	} else {
		log.Printf("Analysis result: %v", issues)
	}

	return nil
}

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

func (p *Processor) ProcessGitLabEvent(eventType string, payload []byte) error {

	return fmt.Errorf("GitLab event processing not implemented")
}
