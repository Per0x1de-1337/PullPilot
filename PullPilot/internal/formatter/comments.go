package formatter

import (
	"fmt"

	"github.com/keploy/PullPilot/pkg/models"
)

func FormatLinterIssue(issue *models.Issue) *models.ReviewComment {
	var emoji string
	switch issue.Severity {
	case models.SeverityError:
		emoji = "🚨"
	case models.SeverityWarning:
		emoji = "⚠️"
	default:
		emoji = "ℹ️"
	}

	body := fmt.Sprintf("%s **%s**\n\n%s", emoji, issue.Title, issue.Description)
	if issue.Suggestion != "" {
		body += "\n\n**Suggestion:** " + issue.Suggestion
	}

	return &models.ReviewComment{
		Path: issue.Path,
		Line: issue.Line,
		Body: body,
	}
}
