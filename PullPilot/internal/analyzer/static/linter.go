package static

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"

	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keploy/PullPilot/internal/config"
	"github.com/keploy/PullPilot/pkg/models"
)

type Linter struct {
	cfg *config.Config
}

var Comment string

func NewLinter(cfg *config.Config) *Linter {
	return &Linter{
		cfg: cfg,
	}
}

func (l *Linter) Analyze(ctx context.Context, files []*models.File) ([]*models.Issue, error) {
	var issues []*models.Issue

	tempDir, err := ioutil.TempDir("", "keploy-review-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	var goFiles, tsFiles, pyFiles, javaFiles []string
	hasGo, hasTS, hasPython, hasJava := false, false, false, false

	for _, file := range files {
		filePath := filepath.Join(tempDir, filepath.Base(file.Path))

		switch {
		case strings.HasSuffix(file.Path, ".go"):
			hasGo = true
			goFiles = append(goFiles, filePath)
		case strings.HasSuffix(file.Path, ".ts"):
			hasTS = true
			tsFiles = append(tsFiles, filePath)
		case strings.HasSuffix(file.Path, ".py"):
			hasPython = true
			pyFiles = append(pyFiles, filePath)
		case strings.HasSuffix(file.Path, ".java"):
			hasJava = true
			javaFiles = append(javaFiles, filePath)

		default:
			continue
		}

		if err := ioutil.WriteFile(filePath, []byte(file.Content), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", file.Path, err)
		}
	}

	if !hasGo && !hasTS && !hasPython && !hasJava {
		log.Println("No Go or TypeScript or python or java files detected in PR")
		Comment = "Add either a Go or TypeScript or python or java file to your PR"
		return issues, nil
	}
	if !hasGo {
		log.Println("No Go files detected in PR")
		Comment = "Add a Go code file to your PR"
	}
	if !hasTS {
		log.Println("No TypeScript files detected in PR")
		Comment = "Add a TypeScript code file to your PR"
	}

	if !hasPython {
		log.Println("No Python files detected in PR")
		Comment = "Add a Python code file to your PR"
	}

	if !hasJava {
		log.Println("No Java files detected in PR")
		Comment = "Add a Java code file to your PR"
	}
	if len(tsFiles) > 0 {
		linterOutput, err := l.RunESLint(ctx, tempDir, tsFiles)
		if err != nil {
			log.Fatalf("Error running ESLint: %v", err)
		}

		issues = append(issues, processLinterOutput(linterOutput, "ESLint")...)
	}
	if len(goFiles) > 0 {
		linterOutput, err := l.runGolangCILint(ctx, tempDir, goFiles)
		if err != nil {
			log.Fatalf("Error running golangci-lint: %v", err)
		}
		issues = append(issues, processLinterOutput(linterOutput, "GolangCILint")...)
	}
	if len(pyFiles) > 0 {
		linterOutput, err := l.RunPythonLinter(ctx, tempDir, pyFiles)
		if err != nil {
			log.Fatalf("Error running flake8: %v", err)
		}
		issues = append(issues, processLinterOutput(linterOutput, "Flake8")...)
	}
	if len(javaFiles) > 0 {
		linterOutput, err := l.RunJavaLinter(ctx, tempDir, javaFiles)
		fmt.Printf("Java Linter Output: %s\n", linterOutput)
		if err != nil {
			log.Fatalf("Error running Checkstyle: %v", err)
		}
		issues = append(issues, processCheckstyleOutput(linterOutput)...)
	}

	fmt.Printf("Total Issues Found: %d\n", len(issues))
	for i, issue := range issues {
		fmt.Printf("Issue %d: %+v\n", i+1, issue)
	}

	return issues, nil
}
func extractJSON(input string) string {
	start := strings.IndexAny(input, "{")
	end := strings.LastIndexAny(input, "}")

	if start == -1 || end == -1 || end < start {
		// Invalid JSON bounds
		return ""
	}

	// Remove the last comma if it exists before the closing brace
	jsonContent := input[start : end+1]
	jsonContent = strings.TrimRight(jsonContent, ",")
	// fmt.Printf("Extracted JSON:///////////////////////////////////////////////////////////////////////\\\\\\\\\\ %s\n", jsonContent)
	return jsonContent
}

func processLinterOutput(output string, fromlinter string) []*models.Issue {
	var issues []*models.Issue
	output = strings.TrimSpace(output)
	if output == "" {
		fmt.Println("Linter output is empty")
		return nil
	}

	jsonStart := strings.IndexAny(output, "{[")
	if jsonStart == -1 {
		fmt.Println("No JSON data found in linter output")
		return nil
	}

	// Use only the JSON part of the output.
	output = output[jsonStart:]
	jsonoutput := extractJSON(output)
	// fmt.Printf("Linter output:**************************************************************************************************************************************************************************************************************************************************************************************************************** %s\n", output)
	fmt.Printf("Extracted JSON:@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@ %s\n", jsonoutput)
	// fmt.Printf("Linting output: %s\n%s\n", jsonoutput, strings.Repeat("[", 100))

	var lintResults struct {
		Issues []struct {
			FromLinter  string   `json:"FromLinter"`
			Text        string   `json:"Text"`
			Severity    string   `json:"Severity"`
			SourceLines []string `json:"SourceLines"`
			Pos         struct {
				Filename string `json:"Filename"`
				Line     int    `json:"Line"`
				Column   int    `json:"Column"`
			} `json:"Pos"`
		} `json:"Issues"`
	}

	if err := json.Unmarshal([]byte(jsonoutput), &lintResults); err != nil {
		fmt.Printf("Error parsing linter output: %v\nOutput: %s\n", err, jsonoutput)
		return nil
	}

	for _, issueData := range lintResults.Issues {
		if issueData.Text == "File ignored because no matching configuration was supplied." {
			continue
		}

		issue := &models.Issue{
			Path:        issueData.Pos.Filename,
			Line:        issueData.Pos.Line,
			Column:      issueData.Pos.Column,
			Severity:    models.Severity(issueData.Severity),
			Title:       fmt.Sprintf("%s Issue: %s", fromlinter, issueData.Text),
			Description: issueData.Text,
			Suggestion:  "Consider fixing this issue based on the linter's feedback.",
			Source:      fromlinter,
		}
		issues = append(issues, issue)
	}

	fmt.Printf("Linting issues are%s\n", jsonoutput)
	fmt.Printf("length of issues: %d\n", len(issues))
	for i, issue := range issues {
		fmt.Printf("Issue %d: %+v\n", i+1, issue)
	}

	return issues
}

func severityToString(severity int) string {
	switch severity {
	case 1:
		return "warning"
	case 2:
		return "error"
	default:
		return "info"
	}
}

func (l *Linter) runGolangCILint(ctx context.Context, dir string, files []string) (string, error) {

	if _, err := exec.LookPath("/snap/bin/golangci-lint"); err != nil {
		fmt.Println("golangci-lint not found:", err)
		return "", nil // Don't stop execution
	}

	configPath := filepath.Join(dir, ".golangci.yml")
	configContent := `
version: 2 
linters:
  enable:
    - govet
    - staticcheck
    - errcheck
    - ineffassign
    - unused
    - misspell
    - gocritic
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		fmt.Println("Failed to write golangci-lint config:", err)
		return "", nil // Don't stop execution
	}

	cmdArgs := []string{
		"/snap/bin/golangci-lint", "run",
		"--config", configPath,
		"--output.json.path", "stdout",
	}
	cmdArgs = append(cmdArgs, files...)

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		fmt.Println("golangci-lint encountered an error:", err)
	}

	return string(output), nil
}

func (l *Linter) RunESLint(ctx context.Context, dir string, files []string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no files provided for ESLint")
	}

	configPath := filepath.Join(dir, "eslint.config.js")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configContent :=
			`
		export default [
  {
    ignores: [],
    files: ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"],
    languageOptions: {
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    rules: {
      "no-unused-vars": "warn",
      "no-console": "warn",
    },
  },
];
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			return "", fmt.Errorf("failed to create ESLint config file: %w", err)
		}
	}

	packagePath := filepath.Join(dir, "package.json")
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		packageContent := "{\"type\": \"module\"}"
		if err := os.WriteFile(packagePath, []byte(packageContent), 0644); err != nil {
			return "", fmt.Errorf("failed to create package.json: %w", err)
		}
	}

	cmdCheck := exec.CommandContext(ctx, "npm", "list", "@eslint/js")
	cmdCheck.Dir = dir
	if err := cmdCheck.Run(); err != nil {

		cmdInstall := exec.CommandContext(ctx, "npm", "install", "--save-dev", "@eslint/js")
		cmdInstall.Dir = dir
		if err := cmdInstall.Run(); err != nil {
			return "", fmt.Errorf("failed to install ESLint dependencies: %w", err)
		}
	}

	args := append([]string{"--format", "json", "--config", configPath}, files...)
	cmd := exec.CommandContext(ctx, "npx", append([]string{"eslint"}, args...)...)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		fmt.Printf("ESLint encountered issues but continuing: %s\n", err)
	}

	var lintResults []map[string]interface{}
	if jsonErr := json.Unmarshal(out.Bytes(), &lintResults); jsonErr != nil {
		return "", fmt.Errorf("invalid ESLint JSON output: %w\nOutput: %s", jsonErr, out.String())
	}

	fmt.Printf("ESLint output: %s\n", out.String())
	return out.String(), nil
}

func (l *Linter) RunPythonLinter(ctx context.Context, dir string, files []string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no Python files provided for linting")
	}

	// Check if flake8 is installed
	if _, err := exec.LookPath("flake8"); err != nil {
		return "", fmt.Errorf("flake8 not found in PATH: %w", err)
	}

	// Optional: Create a configuration file for flake8
	configPath := filepath.Join(dir, ".flake8")
	configContent := `[flake8]
max-line-length = 120
extend-ignore = E203, W503
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write flake8 config file: %w", err)
	}

	// Prepare flake8 command arguments.
	// If you install a plugin like flake8-json, you can use '--format=json' for easier parsing.
	args := append([]string{"--format=json", "--config", configPath}, files...)
	cmd := exec.CommandContext(ctx, "flake8", args...)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	// Run flake8. Note that flake8 returns a non-zero exit status when issues are found,
	// so we log the error but continue to parse the output.
	if err := cmd.Run(); err != nil {
		fmt.Printf("flake8 encountered issues: %s\n", err)
	}

	// Return the output for further processing (similar to your ESLint output processing)
	return out.String(), nil
}

func (l *Linter) RunJavaLinter(ctx context.Context, dir string, files []string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no Java files provided for Checkstyle")
	}

	// Create default Checkstyle config if not exists
	configPath := filepath.Join(dir, "checkstyle.xml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := `<?xml version="1.0"?>
<!DOCTYPE module PUBLIC
    "-//Checkstyle//DTD Checkstyle Configuration 1.3//EN"
    "https://checkstyle.org/dtds/configuration_1_3.dtd">
<module name="Checker">
  <module name="TreeWalker">
    <module name="AvoidStarImport"/>
    <module name="ConstantName"/>
    <module name="UnusedImports"/>
  </module>
</module>`
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
			return "", fmt.Errorf("failed to create Checkstyle config file: %w", err)
		}
	}

	// Ensure Checkstyle is installed
	if _, err := exec.LookPath("checkstyle"); err != nil {
		return "", fmt.Errorf("checkstyle not found in PATH: %w", err)
	}

	args := append([]string{
		"-f", "xml",
		"-c", configPath,
	}, files...)

	cmd := exec.CommandContext(ctx, "checkstyle", args...)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return "", fmt.Errorf("checkstyle failed: %w", err)
	}

	return out.String(), nil
}

type checkstyleXML struct {
	Files []struct {
		Name   string `xml:"name,attr"`
		Errors []struct {
			Line     int    `xml:"line,attr"`
			Column   int    `xml:"column,attr"`
			Severity string `xml:"severity,attr"`
			Message  string `xml:"message,attr"`
			Source   string `xml:"source,attr"`
		} `xml:"error"`
	} `xml:"file"`
}

func processCheckstyleOutput(output string) []*models.Issue {
	var issues []*models.Issue

	output = strings.TrimSpace(output)
	if output == "" {
		log.Println("Checkstyle output is empty")
		return nil
	}

	var result checkstyleXML
	if err := xml.Unmarshal([]byte(output), &result); err != nil {
		log.Printf("Error parsing Checkstyle output: %v\nOutput: %s", err, output)
		return nil
	}
	fmt.Printf("Checkstyle output: %s\n", output)
	for _, file := range result.Files {
		for _, err := range file.Errors {
			issues = append(issues, &models.Issue{
				Path:        file.Name,
				Line:        err.Line,
				Column:      err.Column,
				Severity:    models.Severity(strings.ToLower(err.Severity)),
				Title:       fmt.Sprintf("Checkstyle Issue: %s", filepath.Base(err.Source)),
				Description: err.Message,
				Suggestion:  "Fix according to Checkstyle rule.",
				Source:      "Checkstyle",
			})
		}
	}

	return issues
}
