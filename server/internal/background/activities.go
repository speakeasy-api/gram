package background

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/background/activities"
)

type Activities struct {
	processDeployment    *activities.ProcessDeployment
	transitionDeployment *activities.TransitionDeployment
}

func NewActivities(logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore) *Activities {
	return &Activities{
		processDeployment:    activities.NewProcessDeployment(logger, db, assetStorage),
		transitionDeployment: activities.NewTransitionDeployment(logger, db),
	}
}

func (a *Activities) TransitionDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID, status string) (*activities.TransitionDeploymentResult, error) {
	return a.transitionDeployment.Do(ctx, projectID, deploymentID, status)
}

func (a *Activities) ProcessDeployment(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	return a.processDeployment.Do(ctx, projectID, deploymentID)
}
