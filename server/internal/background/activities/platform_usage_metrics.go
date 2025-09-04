package activities

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

	var wg sync.WaitGroup

	for _, metric := range metrics {
		wg.Add(1)
		go func(m PlatformUsageMetrics) {
			defer wg.Done()
			f.billingTracker.TrackPlatformUsage(ctx, billing.PlatformUsageEvent{
				OrganizationID:      m.OrganizationID,
				PublicMCPServers:    m.PublicMCPServers,
				PrivateMCPServers:   m.PrivateMCPServers,
				TotalEnabledServers: m.TotalEnabledServers,
				TotalTools:          m.TotalTools,
			})
		}(metric)
	}

	wg.Wait()

	f.logger.InfoContext(ctx, "Platform usage metrics firing completed successfully")
	return nil
}
