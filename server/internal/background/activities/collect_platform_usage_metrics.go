package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

type CollectPlatformUsageMetrics struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	usageClient *usage.PolarClient
	repo           *repo.Queries
}

func NewCollectPlatformUsageMetrics(logger *slog.Logger, db *pgxpool.Pool, usageClient *usage.PolarClient) *CollectPlatformUsageMetrics {
	return &CollectPlatformUsageMetrics{
		logger:      logger.With(attr.SlogComponent("collect-platform-usage-metrics")),
		db:          db,
		usageClient: usageClient,
		repo:        repo.New(db),
	}
}

type PlatformUsageMetrics struct {
	OrganizationID    string
	PublicMCPServers  int64
	PrivateMCPServers int64
	TotalToolsets     int64
	TotalTools        int64
}

func (c *CollectPlatformUsageMetrics) Do(ctx context.Context) error {
	c.logger.InfoContext(ctx, "Starting platform usage metrics collection")

	// Query to get comprehensive platform usage metrics per organization

	metrics, err := c.repo.GetPlatformUsageMetrics(ctx)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to get platform usage metrics", attr.SlogError(err))
		return fmt.Errorf("failed to get platform usage metrics: %w", err)
	}

	for _, metric := range metrics {
		go c.usageClient.TrackPlatformUsage(context.Background(), usage.PlatformUsageEvent{
			OrganizationID:    metric.OrganizationID,
			PublicMCPServers:  metric.PublicMcpServers,
			PrivateMCPServers: metric.PrivateMcpServers,
			TotalToolsets:     metric.TotalToolsets,
			TotalTools:        metric.TotalTools,
		})
	}

	c.logger.InfoContext(ctx, "Platform usage metrics collection completed successfully")
	return nil
}
