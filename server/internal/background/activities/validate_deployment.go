package activities

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

type ValidateDeployment struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	billingRepo billing.Repository
}

func NewValidateDeployment(
	logger *slog.Logger,
	db *pgxpool.Pool,
	billingRepo billing.Repository,
) *ValidateDeployment {
	return &ValidateDeployment{
		logger:      logger,
		db:          db,
		billingRepo: billingRepo,
	}
}

func (v *ValidateDeployment) Do(ctx context.Context, projectID uuid.UUID, deploymentID uuid.UUID) error {
	deploymentsRepo := deploymentsrepo.New(v.db)
	projRepo := projectsRepo.New(v.db)
	orgRepo := orgsRepo.New(v.db)

	deployment, err := mv.DescribeDeployment(ctx, v.logger, deploymentsRepo, mv.ProjectID(projectID), mv.DeploymentID(deploymentID))
	if err != nil {
		return err
	}

	orgData, err := projRepo.GetProjectWithOrganizationMetadata(ctx, uuid.MustParse(deployment.ProjectID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading organization metadata").Log(ctx, v.logger)
	}

	org, err := mv.DescribeOrganization(ctx, v.logger, orgRepo, v.billingRepo, orgData.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading organization metadata").Log(ctx, v.logger)
	}

	var validationError *oops.ShareableError

	switch billing.Tier(org.GramAccountType) {
	case billing.TierBase:
		if len(deployment.FunctionsAssets) > 5 {
			validationError = oops.E(oops.CodeForbidden, nil, "Free tier only allows up to 5 function sources. Please contact Speakeasy support for assistance.").Log(ctx, v.logger)
		}
	case billing.TierPro:
		if len(deployment.FunctionsAssets) > 10 {
			validationError = oops.E(oops.CodeForbidden, nil, "Pro tier only allows up to 10 function sources. Please contact Speakeasy support for assistance.").Log(ctx, v.logger)
		}
	case billing.TierEnterprise:
		if len(deployment.FunctionsAssets) > 25 {
			validationError = oops.E(oops.CodeForbidden, nil, "Enterprise tier only allows up to 25 function sources. Please contact Speakeasy support for assistance.").Log(ctx, v.logger)
		}
	default:
		validationError = oops.E(oops.CodeForbidden, nil, "Unsupported organization tier").Log(ctx, v.logger)
	}

	if validationError != nil {
		logErr := deploymentsRepo.LogDeploymentEvent(ctx, deploymentsrepo.LogDeploymentEventParams{
			DeploymentID:   deploymentID,
			ProjectID:      projectID,
			Event:          "log:error",
			Message:        validationError.Error(),
			AttachmentID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			AttachmentType: conv.ToPGTextEmpty(""),
		})

		if logErr != nil {
			v.logger.ErrorContext(ctx, "error logging deployment event", attr.SlogError(logErr))
		}

		return validationError
	}

	return nil
}
