package deployments

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/internal/deployments/repo"
)

func (s *Service) logDeploymentError(ctx context.Context, logger *slog.Logger, tx *repo.Queries, projectID uuid.UUID, deploymentID uuid.UUID, message string) error {
	err := tx.LogDeploymentEvent(ctx, repo.LogDeploymentEventParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
		Event:        "deployment:error",
		Message:      message,
	})

	if err != nil {
		logger.ErrorContext(ctx, "failed to log deployment error", slog.String("error", err.Error()))
		return errors.New("unexpected database error (log)")
	}

	return nil
}
