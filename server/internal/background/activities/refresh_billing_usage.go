package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type RefreshBillingUsage struct {
	logger      *slog.Logger
	billingRepo billing.Repository
	repo        *repo.Queries
	orgRepo     *orgRepo.Queries
}

func NewRefreshBillingUsage(logger *slog.Logger, db *pgxpool.Pool, billingRepo billing.Repository) *RefreshBillingUsage {
	repo := repo.New(db)
	orgRepo := orgRepo.New(db)

	return &RefreshBillingUsage{
		logger:      logger,
		billingRepo: billingRepo,
		repo:        repo,
		orgRepo:     orgRepo,
	}
}

// Refresh billing usage for a list of organizations
// Send usage data to posthog for tracking purposes

func (r *RefreshBillingUsage) Do(ctx context.Context, orgIDs []string) error {
	workers := pool.New().WithErrors().WithMaxGoroutines(25)

	for _, orgID := range orgIDs {
		workers.Go(func() error {
			orgLogger := r.logger.With(attr.SlogOrganizationID(orgID))

			// significant to refresh polar related caching
			org, err := mv.DescribeOrganization(ctx, r.logger, r.orgRepo, r.billingRepo, orgID)
			if err != nil {
				orgLogger.ErrorContext(ctx, "failed to describe organization", attr.SlogError(err))
				return fmt.Errorf("failed to describe organization %s: %w", orgID, err)
			}

			orgLogger = orgLogger.With(attr.SlogOrganizationSlug(org.Slug))

			// we refresh the period usage data store up to date at least hourly
			if _, err := r.billingRepo.GetPeriodUsage(ctx, orgID); err != nil {
				orgLogger.ErrorContext(ctx, "failed to get period usage", attr.SlogError(err))
				return fmt.Errorf("failed to get period usage for org %s (%s): %w", org.Slug, orgID, err)
			}

			return nil
		})
	}

	if err := workers.Wait(); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to refresh billing usage").Log(ctx, r.logger)
	}

	return nil
}
