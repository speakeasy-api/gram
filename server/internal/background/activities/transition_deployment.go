package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
)

type TransitionDeployment struct {
	db     *pgxpool.Pool
	repo   *repo.Queries
	logger *slog.Logger
}

func NewTransitionDeployment(logger *slog.Logger, db *pgxpool.Pool) *TransitionDeployment {
	return &TransitionDeployment{
		db:     db,
		repo:   repo.New(db),
		logger: logger,
	}
}

type TransitionDeploymentResult struct {
	Status string
	Moved  bool
}

func (t *TransitionDeployment) Do(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, status string) (*TransitionDeploymentResult, error) {
	state, err := t.repo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: deploymentID,
		Status:       status,
		ProjectID:    projectID,
		Event:        "deployment:status_change",
		Message:      fmt.Sprintf("Deployment moved to %s state", status),
	})
	if err != nil {
		return nil, err
	}

	return &TransitionDeploymentResult{
		Status: state.Status,
		Moved:  state.Moved,
	}, nil
}
