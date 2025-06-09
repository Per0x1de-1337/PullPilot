package custom

import (
	"context"

	"github.com/keploy/PullPilot/internal/config"
	"github.com/keploy/PullPilot/pkg/models"
)

type Rules struct {
	cfg *config.Config
}

func NewRules(cfg *config.Config) *Rules {
	return &Rules{
		cfg: cfg,
	}
}

func (r *Rules) Analyze(ctx context.Context, files []*models.File) ([]*models.Issue, error) {
	var issues []*models.Issue

	return issues, nil
}
