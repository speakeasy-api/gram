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
		return oops.E(oops.CodeUnexpected, err, "error loading organization metadata").LogError(ctx, v.logger)
	}

	org, err := mv.DescribeOrganization(ctx, v.logger, orgRepo, v.billingRepo, orgData.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading organization metadata").LogError(ctx, v.logger)
	}

	var validationError *oops.ShareableError
	tierLimit, ok := deploymentLimitForTier(billing.Tier(org.GramAccountType))
	switch {
	case !ok:
		validationError = oops.E(oops.CodeForbidden, nil, "Unsupported organization tier").LogError(ctx, v.logger)
	case len(deployment.FunctionsAssets) > tierLimit.maxFunctionAssets:
		validationError = oops.E(
			oops.CodeForbidden,
			nil,
			"%s tier only allows up to %d function sources. Please contact Speakeasy support for assistance.",
			tierLimit.displayName,
			tierLimit.maxFunctionAssets,
		).LogError(ctx, v.logger)
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

type deploymentTierLimit struct {
	displayName       string
	maxFunctionAssets int
}

func deploymentLimitForTier(tier billing.Tier) (deploymentTierLimit, bool) {
	switch tier {
	case billing.TierBase:
		return deploymentTierLimit{displayName: "Free", maxFunctionAssets: 5}, true
	case billing.TierPro:
		return deploymentTierLimit{displayName: "Pro", maxFunctionAssets: 10}, true
	case billing.TierPayg:
		return deploymentTierLimit{displayName: "PAYG", maxFunctionAssets: 25}, true
	case billing.TierEnterprise:
		return deploymentTierLimit{displayName: "Enterprise", maxFunctionAssets: 25}, true
	default:
		return deploymentTierLimit{displayName: "", maxFunctionAssets: 0}, false
	}
}
