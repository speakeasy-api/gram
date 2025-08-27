package activities

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const (
	slackTeamID    = "T02F97J9JMV"
	slackChannelID = "C091P48QFEZ"
)

type ReportFreeTierOverage struct {
	logger        *slog.Logger
	usageClient   *polar.Client
	repo          *repo.Queries
	orgRepo       *orgRepo.Queries
	posthogClient *posthog.Posthog
}

func NewReportFreeTierOverage(logger *slog.Logger, db *pgxpool.Pool, usageClient *polar.Client, posthogClient *posthog.Posthog) *ReportFreeTierOverage {
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
		return err
	}

	for _, org := range orgs {
		org, err := mv.DescribeOrganization(ctx, r.logger, r.orgRepo, org.ID, r.usageClient)
		if err != nil {
			return err
		}

		// TODO
		// if org.GramAccountType != "free" {
		// 	continue
		// }

		usage, err := r.usageClient.GetPeriodUsage(ctx, org.ID)
		if err != nil {
			return err
		}

		if usage.ToolCalls > usage.MaxToolCalls || usage.Servers > usage.MaxServers {
			r.logger.InfoContext(ctx, "free tier overage", slog.Any("org", org), slog.Any("usage", usage))
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
				return err
			}
		}
	}

	return nil
}
