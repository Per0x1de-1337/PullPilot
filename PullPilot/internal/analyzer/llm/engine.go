package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/keploy/PullPilot/internal/config"
	"github.com/keploy/PullPilot/pkg/models"
)

type Engine struct {
	cfg *config.Config
}

var Comment string

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg: cfg,
	}
}

func (e *Engine) Analyze(ctx context.Context, files []*models.File) ([]*models.Issue, error) {
	var issues []*models.Issue

	if !e.cfg.EnableLLM {
		return issues, nil
	}

	for _, file := range files {

		if len(file.Content) > int(e.cfg.MaxFileSizeBytes) {
			continue
		}

		if !isCodeFile(file.Path) {
			comment := "Add a code file to your PR."

			if comment != "" {
				Comment = comment
				fmt.Printf("comment in isCodefile is non-empty")
			} else {
				Comment = ""
				fmt.Printf("comment in isCodefile in empty")
			}
			continue
		}

		issues = append(issues, &models.Issue{
			Path:        file.Path,
			Line:        1,
			Severity:    models.SeverityInfo,
			Title:       "LLM analysis placeholder",
			Description: "This is a placeholder for LLM-based code analysis",
			Source:      "llm-engine",
		})
	}

	return issues, nil
}

func isCodeFile(path string) bool {
	codeExtensions := []string{".go", ".js", ".ts", ".py", ".java", ".c", ".cpp", ".h"}
	for _, ext := range codeExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
