package reporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/keploy/PullPilot/pkg/models"
)

func GenerateMarkdownReport(issues []*models.Issue) string {
	var builder strings.Builder

	builder.WriteString("# Code Analysis Report\n\n")
	builder.WriteString(fmt.Sprintf("**Generated at**: %s\n\n", time.Now().Format(time.RFC1123)))

	summary := make(map[models.Severity]int)
	for _, issue := range issues {
		summary[issue.Severity]++
	}

	builder.WriteString("## Summary\n")
	builder.WriteString("| Severity | Count |\n")
	builder.WriteString("|----------|-------|\n")
	builder.WriteString(fmt.Sprintf("| 🔴 Critical | %d |\n", summary[models.SeverityError]))
	builder.WriteString(fmt.Sprintf("| 🟠 Warning | %d |\n", summary[models.SeverityWarning]))
	builder.WriteString(fmt.Sprintf("| ℹ️  Info | %d |\n\n", summary[models.SeverityInfo]))

	builder.WriteString("## Detailed Findings\n")

	grouped := make(map[models.Severity][]*models.Issue)
	for _, issue := range issues {
		grouped[issue.Severity] = append(grouped[issue.Severity], issue)
	}

	severities := []models.Severity{
		models.SeverityError,
		models.SeverityWarning,
		models.SeverityInfo,
	}

	for _, severity := range severities {
		if len(grouped[severity]) == 0 {
			continue
		}

		builder.WriteString(fmt.Sprintf("\n### %s %s\n", severityEmoji(severity), severityString(severity)))
		builder.WriteString("| File | Line | Description | Source | Suggestion |\n")
		builder.WriteString("|------|------|-------------|--------|------------|\n")

		for _, issue := range grouped[severity] {
			line := fmt.Sprintf("%d", issue.Line)
			if issue.Line == 0 {
				line = "N/A"
			}

			suggestion := issue.Suggestion
			if suggestion == "" {
				suggestion = "-"
			}

			builder.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n",
				issue.Path,
				line,
				escapeMD(issue.Description),
				issue.Source,
				escapeMD(suggestion),
			))
		}
	}

	return builder.String()
}

func severityString(s models.Severity) string {
	switch s {
	case models.SeverityError:
		return "Critical Issues"
	case models.SeverityWarning:
		return "Warnings"
	default:
		return "Info & Suggestions"
	}
}

func severityEmoji(s models.Severity) string {
	switch s {
	case models.SeverityError:
		return "🔴"
	case models.SeverityWarning:
		return "🟠"
	default:
		return "ℹ️"
	}
}

func escapeMD(text string) string {
	return strings.NewReplacer(
		"|", "\\|",
		"`", "'",
		"\n", "<br>",
	).Replace(text)
}
