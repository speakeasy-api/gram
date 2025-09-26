package activities

import (
	"context"
	"crypto/rand"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type ProvisionFunctionsAccess struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewProvisionFunctionsAccess(
	logger *slog.Logger,
	db *pgxpool.Pool,
) *ProvisionFunctionsAccess {
	return &ProvisionFunctionsAccess{
		logger: logger.With(attr.SlogComponent("get-all-organizations")),
		db:     db,
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
		return oops.E(oops.CodeInvalid, err, "invalid inputs").Log(ctx, logger)
	}

	dbtx, err := p.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing deployments").Log(ctx, p.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	deprepo := repo.New(dbtx)
	attachments, err := deprepo.GetDeploymentFunctionsWithoutAccess(ctx, repo.GetDeploymentFunctionsWithoutAccessParams{
		DeploymentID: deploymentID,
		ProjectID:    projectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get functions missing access").Log(ctx, logger)
	}

	for _, aid := range attachments {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to generate encryption key").Log(ctx, logger)
		}

		_, err = deprepo.CreateDeploymentFunctionsAccess(ctx, repo.CreateDeploymentFunctionsAccessParams{
			ProjectID:     projectID,
			DeploymentID:  deploymentID,
			FunctionID:    aid,
			EncryptionKey: key,
			BearerFormat:  conv.ToPGText("GramV1"),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to create functions access").Log(ctx, logger, attr.SlogDeploymentFunctionsID(aid.String()))
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error committing functions access creation").Log(ctx, logger)
	}

	return nil
}
