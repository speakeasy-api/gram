package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/mv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const (
	logKeyOrgID     = "org_id"
	logKeyOrgName   = "org_name"
	logKeyToolCalls = "tool_calls"
	logKeyServers   = "servers"
)

type ReportBillingUsage struct {
	logger        *slog.Logger
	billingRepo   billing.Repository
	repo          *repo.Queries
	orgRepo       *orgRepo.Queries
	posthogClient *posthog.Posthog
}

func NewReportBillingUsage(logger *slog.Logger, db *pgxpool.Pool, billingRepo billing.Repository, posthogClient *posthog.Posthog) *ReportBillingUsage {
	repo := repo.New(db)
	orgRepo := orgRepo.New(db)

	return &ReportBillingUsage{
		logger:        logger,
		billingRepo:   billingRepo,
		repo:          repo,
		orgRepo:       orgRepo,
		posthogClient: posthogClient,
	}
}

// Report billing usage for a list of organizations
// Send usage data to posthog for tracking purposes

func (r *ReportBillingUsage) Do(ctx context.Context, orgIDs []string) error {
	for _, orgID := range orgIDs {
		org, err := mv.DescribeOrganization(ctx, r.logger, r.orgRepo, r.billingRepo, orgID)
		if err != nil {
			return fmt.Errorf("failed to describe organization %s: %w", orgID, err)
		}

		usage, err := r.billingRepo.GetPeriodUsage(ctx, orgID)
		if err != nil {
			return fmt.Errorf("failed to get period usage for org %s: %w", orgID, err)
		}

		r.logger.InfoContext(ctx, "billing usage report",
			slog.String(logKeyOrgID, org.ID),
			slog.String(logKeyOrgName, org.Name),
			slog.Int(logKeyToolCalls, usage.ToolCalls),
			slog.Int(logKeyServers, usage.Servers),
		)

		err = r.posthogClient.CaptureEvent(ctx, "billing_usage_report", org.ID, map[string]any{
			"org_id":     org.ID,
			"org_name":   org.Name,
			"org_slug":   org.Slug,
			"tool_calls": usage.ToolCalls,
			"servers":    usage.Servers,
			"is_gram":    true,
		})
		if err != nil {
			return fmt.Errorf("failed to capture posthog event for org %s: %w", orgID, err)
		}
	}

	return nil
}
