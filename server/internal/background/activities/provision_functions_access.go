package activities

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/functions/runnerkeys"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type ProvisionFunctionsAccess struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	keys   *runnerkeys.Keys
}

func NewProvisionFunctionsAccess(
	logger *slog.Logger,
	db *pgxpool.Pool,
	keys *runnerkeys.Keys,
) *ProvisionFunctionsAccess {
	return &ProvisionFunctionsAccess{
		logger: logger.With(attr.SlogComponent("provision_functions_access")),
		db:     db,
		keys:   keys,
	}
}

func (p *ProvisionFunctionsAccess) Do(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	logger := p.logger.With(
		attr.SlogProjectID(projectID.String()),
		attr.SlogDeploymentID(deploymentID.String()),
	)

	if err := inv.Check(
		"provision functions access inputs",
		"project id cannot be nil", projectID != uuid.Nil,
		"deployment id cannot be nil", deploymentID != uuid.Nil,
	); err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid inputs").LogError(ctx, logger)
	}

	attachments, err := repo.New(p.db).GetDeploymentFunctionsWithoutAccess(ctx, repo.GetDeploymentFunctionsWithoutAccessParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get functions missing access").LogError(ctx, logger)
	}
	if len(attachments) == 0 {
		return nil
	}

	// A failed run leaves some functions without access; the retry fills them.
	if _, err := p.keys.Create(ctx, p.db, projectID, deploymentID, attachments); err != nil {
		return runnerkeys.Error(ctx, logger, err, "failed to create functions access")
	}

	return nil
}
