package deployments

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/oops"
)

func (s *Service) logDeploymentError(ctx context.Context, logger *slog.Logger, tx *repo.Queries, projectID uuid.UUID, deploymentID uuid.UUID, message string) error {
	err := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
		Event:        "deployment:error",
		Message:      message,
	})

	if err != nil {
		return oops.E(err, "unexpected database error", "failed to log deployment error").Log(ctx, logger)
	}

	return nil
}
