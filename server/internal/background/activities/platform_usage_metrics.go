package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sourcegraph/conc/pool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const (
	logKeyOrgID     = "org_id"
	logKeyOrgName   = "org_name"
	logKeyToolCalls = "tool_calls"
	logKeyServers   = "servers"
)

type CollectPlatformUsageMetrics struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
}

func NewCollectPlatformUsageMetrics(logger *slog.Logger, db *pgxpool.Pool) *CollectPlatformUsageMetrics {
	return &CollectPlatformUsageMetrics{
		logger: logger.With(attr.SlogComponent("collect-platform-usage-metrics")),
		db:     db,
		repo:   repo.New(db),
	}
}

type PlatformUsageMetrics struct {
	OrganizationID      string
	PublicMCPServers    int64
	PrivateMCPServers   int64
	TotalEnabledServers int64
	TotalToolsets       int64
	TotalTools          int64
}

func (c *CollectPlatformUsageMetrics) Do(ctx context.Context) ([]PlatformUsageMetrics, error) {
	c.logger.InfoContext(ctx, "Starting platform usage metrics collection")

	rows, err := c.repo.GetPlatformUsageMetrics(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get platform usage metrics").Log(ctx, c.logger)
	}

	metrics := make([]PlatformUsageMetrics, 0, len(rows))
	for _, row := range rows {
		metrics = append(metrics, PlatformUsageMetrics{
			OrganizationID:      row.OrganizationID,
			PublicMCPServers:    row.PublicMcpServers,
			PrivateMCPServers:   row.PrivateMcpServers,
			TotalEnabledServers: row.TotalEnabledServers,
			TotalToolsets:       row.TotalToolsets,
			TotalTools:          row.TotalTools,
		})
	}

	c.logger.InfoContext(ctx, "Platform usage metrics collection completed successfully")
	return metrics, nil
}

type FirePlatformUsageMetrics struct {
	logger         *slog.Logger
	billingTracker billing.Tracker
}

func NewFirePlatformUsageMetrics(logger *slog.Logger, billingTracker billing.Tracker) *FirePlatformUsageMetrics {
	return &FirePlatformUsageMetrics{
		logger:         logger.With(attr.SlogComponent("fire-platform-usage-metrics")),
		billingTracker: billingTracker,
	}
}

func (f *FirePlatformUsageMetrics) Do(ctx context.Context, metrics []PlatformUsageMetrics) error {
	f.logger.InfoContext(ctx, "Starting platform usage metrics firing")

	events := make([]billing.PlatformUsageEvent, 0, len(metrics))

	for _, metric := range metrics {
		events = append(events, billing.PlatformUsageEvent{
			OrganizationID:      metric.OrganizationID,
			PublicMCPServers:    metric.PublicMCPServers,
			PrivateMCPServers:   metric.PrivateMCPServers,
			TotalEnabledServers: metric.TotalEnabledServers,
			TotalToolsets:       metric.TotalToolsets,
			TotalTools:          metric.TotalTools,
		})
	}

	f.billingTracker.TrackPlatformUsage(ctx, events)

	f.logger.InfoContext(ctx, "Platform usage metrics firing completed successfully")
	return nil
}

type FreeTierReportingUsageMetrics struct {
	logger            *slog.Logger
	billingRepository billing.Repository
	orgRepo           *orgRepo.Queries
	posthogClient     *posthog.Posthog
}

func NewFreeTierReportingMetrics(logger *slog.Logger, db *pgxpool.Pool, billingRepository billing.Repository, posthogClient *posthog.Posthog) *FreeTierReportingUsageMetrics {
	return &FreeTierReportingUsageMetrics{
		logger:            logger.With(attr.SlogComponent("free-tier-reporting-usage-metrics")),
		billingRepository: billingRepository,
		orgRepo:           orgRepo.New(db),
		posthogClient:     posthogClient,
	}
}

func (f *FreeTierReportingUsageMetrics) Do(ctx context.Context, orgIDs []string) error {
	f.logger.InfoContext(ctx, "Starting free tier reporting usage metrics")

	workers := pool.New().WithErrors().WithMaxGoroutines(25)

	for _, orgID := range orgIDs {
		workers.Go(func() error {
			org, err := mv.DescribeOrganization(ctx, f.logger, f.orgRepo, f.billingRepository, orgID)
			if err != nil {
				return fmt.Errorf("failed to describe organization %s: %w", orgID, err)
			}

			// get latest period usage that was stored
			usage, err := f.billingRepository.GetStoredPeriodUsage(ctx, orgID)
			if err != nil {
				f.logger.ErrorContext(ctx, "failed to get period usage for org", attr.SlogError(err), attr.SlogOrganizationID(orgID))
				return nil
			}

			f.logger.InfoContext(ctx, "billing usage report",
				slog.String(logKeyOrgID, org.ID),
				slog.String(logKeyOrgName, org.Name),
				slog.Int(logKeyToolCalls, usage.ToolCalls),
				slog.Int(logKeyServers, usage.Servers),
			)

			if org.GramAccountType == "free" && (usage.ToolCalls > usage.MaxToolCalls || usage.Servers > usage.MaxServers) {
				err = f.posthogClient.CaptureEvent(ctx, "billing_usage_report", org.ID, map[string]any{
					"org_id":        org.ID,
					"org_name":      org.Name,
					"org_slug":      org.Slug,
					"tool_calls":    usage.ToolCalls,
					"servers":       usage.Servers,
					"is_gram":       true,
					"is_legacy_org": org.CreatedAt.Time.Before(time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC)), // This is when free tier limit enforcement started
				})
				if err != nil {
					return fmt.Errorf("failed to capture posthog event for org %s: %w", orgID, err)
				}
			}

			return nil
		})
	}

	if err := workers.Wait(); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to report free tier usage").Log(ctx, f.logger)
	}

	f.logger.InfoContext(ctx, "free tier reporting usage metrics completed successfully")
	return nil
}
