package shared

import (
	"fmt"

	"github.com/keploy/PullPilot/pkg/models"
)

var AllIssues []*models.Issue

func GetAllIssues() []*models.Issue {
	return AllIssues
}

func AddIssue(issue *models.Issue) error {
	fmt.Printf("Issue: %+v\n", issue)
	AllIssues = append(AllIssues, issue)
	return nil
}
