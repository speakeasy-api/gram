package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	if status == "completed" {
		return t.complete(ctx, projectID, deploymentID, nil)
	}

	return t.transition(ctx, t.repo, projectID, deploymentID, status, fmt.Sprintf("Deployment moved to %s state", status))
}

func (t *TransitionDeployment) CompleteWithExpectedActive(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, expectedActive uuid.UUID) (*TransitionDeploymentResult, error) {
	return t.complete(ctx, projectID, deploymentID, &expectedActive)
}

func (t *TransitionDeployment) complete(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, expectedActive *uuid.UUID) (*TransitionDeploymentResult, error) {
	tx, err := t.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting deployment transition transaction").LogError(ctx, t.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// Legacy TransitionDeployment also inserts a deployment_log in the same
	// statement. Its existing project foreign key takes KEY SHARE on this row,
	// which conflicts with FOR UPDATE, so old workers serialize here as well.
	if _, err := projectsrepo.New(tx).GetProjectByIDForUpdate(ctx, projectID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking project for deployment transition").LogError(ctx, t.logger)
	}

	deploymentRepo := repo.New(tx)
	status := "completed"
	message := "Deployment moved to completed state"
	if expectedActive != nil {
		deployment, err := deploymentRepo.GetDeployment(ctx, repo.GetDeploymentParams{ID: deploymentID, ProjectID: projectID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error getting deployment for guarded completion").LogError(ctx, t.logger)
		}
		if deployment.Status == "completed" || deployment.Status == "failed" {
			if err := tx.Commit(ctx); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error committing deployment transition").LogError(ctx, t.logger)
			}
			return &TransitionDeploymentResult{Status: deployment.Status, Moved: true}, nil
		}

		activeDeploymentID, err := deploymentRepo.GetActiveDeploymentID(ctx, projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			activeDeploymentID = uuid.Nil
		} else if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error getting active deployment").LogError(ctx, t.logger)
		}

		if activeDeploymentID != *expectedActive {
			status = "failed"
			message = "Deployment failed because the active deployment changed while the redeploy was processing"
		}
	}

	result, err := t.transition(ctx, deploymentRepo, projectID, deploymentID, status, message)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing deployment transition").LogError(ctx, t.logger)
	}

	return result, nil
}

func (t *TransitionDeployment) transition(ctx context.Context, deploymentRepo *repo.Queries, projectID uuid.UUID, deploymentID uuid.UUID, status string, message string) (*TransitionDeploymentResult, error) {
	state, err := deploymentRepo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: deploymentID,
		Status:       status,
		ProjectID:    projectID,
		Event:        "deployment:status_change",
		Message:      message,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error transitioning deployment").LogError(ctx, t.logger)
	}

	return &TransitionDeploymentResult{
		Status: state.Status,
		Moved:  state.Moved,
	}, nil
}
