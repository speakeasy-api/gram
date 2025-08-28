package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/types"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const (
	logKeyOrgID        = "org_id"
	logKeyOrgName      = "org_name"
	logKeyToolCalls    = "tool_calls"
	logKeyMaxToolCalls = "max_tool_calls"
	logKeyServers      = "servers"
	logKeyMaxServers   = "max_servers"
)

type ReportFreeTierOverage struct {
	logger        *slog.Logger
	usageClient   usage_types.UsageClient
	repo          *repo.Queries
	orgRepo       *orgRepo.Queries
	posthogClient *posthog.Posthog
}

func NewReportFreeTierOverage(logger *slog.Logger, db *pgxpool.Pool, usageClient usage_types.UsageClient, posthogClient *posthog.Posthog) *ReportFreeTierOverage {
	repo := repo.New(db)
	orgRepo := orgRepo.New(db)

	return &ReportFreeTierOverage{
		logger:        logger,
		usageClient:   usageClient,
		repo:          repo,
		orgRepo:       orgRepo,
		posthogClient: posthogClient,
	}
}

// Loop through all orgs and check in Polar if they are over the free tier limits
// If they are, send a posthog event so that it can be forwarded to the slack channel

func (r *ReportFreeTierOverage) Do(ctx context.Context) error {
	orgs, err := r.repo.GetAllOrganizations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all organizations: %w", err)
	}

	for _, org := range orgs {
		org, err := mv.DescribeOrganization(ctx, r.logger, r.orgRepo, org.ID, r.usageClient)
		if err != nil {
			return err
		}

		if org.GramAccountType != "free" {
			continue
		}

		usage, err := r.usageClient.GetPeriodUsage(ctx, org.ID)
		if err != nil {
			return fmt.Errorf("failed to get period usage for org %s: %w", org.ID, err)
		}

		if usage.ToolCalls > usage.MaxToolCalls || usage.Servers > usage.MaxServers {
			r.logger.InfoContext(ctx, "free tier overage",
				slog.String(logKeyOrgID, org.ID),
				slog.String(logKeyOrgName, org.Name),
				slog.Int(logKeyToolCalls, usage.ToolCalls),
				slog.Int(logKeyMaxToolCalls, usage.MaxToolCalls),
				slog.Int(logKeyServers, usage.Servers),
				slog.Int(logKeyMaxServers, usage.MaxServers),
			)
			err = r.posthogClient.CaptureEvent(ctx, "free_tier_overage", org.ID, map[string]any{
				"org_id":         org.ID,
				"org_name":       org.Name,
				"org_slug":       org.Slug,
				"tool_calls":     usage.ToolCalls,
				"max_tool_calls": usage.MaxToolCalls,
				"servers":        usage.Servers,
				"max_servers":    usage.MaxServers,
				"is_gram":        true,
			})
			if err != nil {
				return fmt.Errorf("failed to capture posthog event for org %s: %w", org.ID, err)
			}
		}
	}

	return nil
}
