package dependency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/keploy/PullPilot/internal/config"
	"github.com/keploy/PullPilot/pkg/models"
)

type Scanner struct {
	cfg    *config.Config
	client *http.Client
}

func NewScanner(cfg *config.Config) *Scanner {
	fmt.Println("********************************************************************************")
	fmt.Println("Initializing Scanner with config:", cfg)
	fmt.Println("********************************************************************************")

	return &Scanner{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Scanner) Analyze(ctx context.Context, files []*models.File) ([]*models.Issue, error) {
	fmt.Println("********************************************************************************")
	fmt.Println("Starting dependency analysis...")
	fmt.Println("********************************************************************************")

	var issues []*models.Issue

	for _, file := range files {
		fmt.Printf("Processing file: %s\n", file.Path)

		switch filepath.Base(file.Path) {
		case "go.mod":
			fmt.Println("Detected Go module file.")
			deps := parseGoMod(file.Content)
			fmt.Println("Parsed dependencies:", deps)
			issues = append(issues, s.checkDeps(ctx, "go", deps)...)

		case "package.json":
			fmt.Println("Detected package.json file.")
			deps := parsePackageJSON(file.Content)
			fmt.Println("Parsed dependencies:", deps)
			issues = append(issues, s.checkDeps(ctx, "npm", deps)...)

		default:
			fmt.Println("Skipping file:", file.Path)
		}
	}

	fmt.Println("********************************************************************************")
	fmt.Println("Dependency analysis completed. Issues found:", issues)
	fmt.Println("********************************************************************************")

	return issues, nil
}

func (s *Scanner) checkDeps(ctx context.Context, ecosystem string, deps map[string]string) []*models.Issue {
	fmt.Println("********************************************************************************")
	fmt.Printf("Checking dependencies for ecosystem: %s\n", ecosystem)
	fmt.Println("********************************************************************************")

	var issues []*models.Issue

	for pkg, version := range deps {
		cleanVersion := strings.TrimLeft(version, "^~") // Remove ^ and ~
		escapedPkg := url.PathEscape(pkg)
		queryURL := fmt.Sprintf("https://api.deps.dev/v3/systems/%s/packages/%s/versions/%s",
			ecosystem, escapedPkg, url.PathEscape(cleanVersion))

		fmt.Printf("Checking package: %s, original version: %s, cleaned version: %s\n", pkg, version, cleanVersion)
		fmt.Println("Requesting URL:", queryURL)

		resp, err := s.client.Get(queryURL)
		if err != nil {
			fmt.Println("Error fetching dependency info:", err)
			continue
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			fmt.Println("Error decoding JSON response:", err)
			continue
		}

		responseJSON, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(responseJSON))

		advisoryKeys, exists := result["advisoryKeys"].([]interface{})
		if !exists || len(advisoryKeys) == 0 {
			fmt.Println("No advisories found for", pkg)
			continue
		}

		for _, adv := range advisoryKeys {
			advMap, ok := adv.(map[string]interface{})
			if !ok || advMap["id"] == nil {
				continue
			}
			advisoryID := advMap["id"].(string)
			advisoryURL := fmt.Sprintf("https://api.deps.dev/v3alpha/advisories/%s", advisoryID)
			fmt.Println("Fetching advisory details for:", advisoryID)

			advResp, err := s.client.Get(advisoryURL)
			if err != nil {
				fmt.Println("Error fetching advisory details:", err)
				continue
			}
			defer advResp.Body.Close()

			var advDetail map[string]interface{}
			if err := json.NewDecoder(advResp.Body).Decode(&advDetail); err != nil {
				fmt.Println("Error decoding advisory details:", err)
				continue
			}
			fmt.Printf("Advisory details for %s: %v\n", advisoryID, advDetail)

			cvssScore := 0.0                                                // Use a zero value for float64
			if score, exists := advDetail["cvss3Score"].(float64); exists { // Adjusted to access directly as float64
				cvssScore = score
			} else if cvss, ok := advDetail["cvss3Score"].(map[string]interface{}); ok {
				fmt.Printf("CVSS Score raw (map): %v\n", cvss)
				if score, exists := cvss["score"].(float64); exists {
					cvssScore = score
				}
			}

			fmt.Println("CVSS Score:", cvssScore)

			title := advDetail["title"].(string)
			fmt.Printf("Found vulnerability in %s: %s (CVSS: %.1f)\n", pkg, title, cvssScore)

			if cvssScore >= 7.0 {
				issue := &models.Issue{
					Path:        pkg,
					Title:       "Vulnerable Dependency",
					Description: fmt.Sprintf("%s (CVSS: %.1f)", title, cvssScore),
					Severity:    models.SeverityError,
					Source:      "deps.dev",
				}
				issues = append(issues, issue)
			}
		}
	}

	fmt.Println("********************************************************************************")

	for _, issue := range issues {
		fmt.Printf("Issue: %s, %s, %s, %s\n", issue.Path, issue.Title, issue.Description, issue.Severity)
	}
	fmt.Println("********************************************************************************")
	fmt.Println("Dependency check completed. Total issues:", len(issues))
	fmt.Println("********************************************************************************")

	return issues
}

func parseGoMod(content string) map[string]string {
	fmt.Println("********************************************************************************")
	fmt.Println("Parsing go.mod file content...")
	fmt.Println("********************************************************************************")

	deps := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "require ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				deps[parts[1]] = parts[2]
				fmt.Printf("Parsed dependency: %s -> %s\n", parts[1], parts[2])
			}
		}
	}

	fmt.Println("Parsed go.mod dependencies:", deps)
	fmt.Println("********************************************************************************")
	return deps
}

func parsePackageJSON(content string) map[string]string {
	fmt.Println("********************************************************************************")
	fmt.Println("Parsing package.json file content...")
	fmt.Println("********************************************************************************")

	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		fmt.Println("Error parsing package.json:", err)
		return nil
	}

	fmt.Println("Parsed package.json dependencies:", pkg.Dependencies)
	fmt.Println("********************************************************************************")
	return pkg.Dependencies
}
