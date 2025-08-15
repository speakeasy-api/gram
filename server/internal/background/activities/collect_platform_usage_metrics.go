package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

const (
	publicMCPServersKey  = "public_mcp_servers"
	privateMCPServersKey = "private_mcp_servers"
	totalToolsetsKey     = "total_toolsets"
	totalToolsKey        = "total_tools"
)

type CollectPlatformUsageMetrics struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	usageClient *usage.PolarClient
}

func NewCollectPlatformUsageMetrics(logger *slog.Logger, db *pgxpool.Pool, usageClient *usage.PolarClient) *CollectPlatformUsageMetrics {
	return &CollectPlatformUsageMetrics{
		logger:      logger.With(attr.SlogComponent("collect-platform-usage-metrics")),
		db:          db,
		usageClient: usageClient,
	}
}

type PlatformUsageMetrics struct {
	OrganizationID      string
	PublicMCPServers    int64
	PrivateMCPServers   int64
	TotalToolsets       int64
	TotalTools          int64
}

func (c *CollectPlatformUsageMetrics) Do(ctx context.Context) error {
	c.logger.InfoContext(ctx, "Starting platform usage metrics collection")

	// Query to get comprehensive platform usage metrics per organization
	query := `
		WITH latest_deployments AS (
			SELECT DISTINCT ON (project_id) project_id, id as deployment_id
			FROM deployments 
			ORDER BY project_id, created_at DESC
		),
		toolset_metrics AS (
			SELECT 
				p.organization_id,
				COUNT(CASE WHEN t.mcp_is_public = true AND t.mcp_slug IS NOT NULL THEN 1 END) as public_mcp_servers,
				COUNT(CASE WHEN t.mcp_is_public = false AND t.mcp_slug IS NOT NULL THEN 1 END) as private_mcp_servers,
				COUNT(t.id) as total_toolsets
			FROM projects p
			LEFT JOIN toolsets t ON p.id = t.project_id AND t.deleted = false
			GROUP BY p.organization_id
		),
		tool_metrics AS (
			SELECT 
				p.organization_id,
				COUNT(DISTINCT htd.id) as total_tools
			FROM projects p
			LEFT JOIN latest_deployments ld ON p.id = ld.project_id
			LEFT JOIN http_tool_definitions htd ON ld.deployment_id = htd.deployment_id AND htd.deleted = false
			GROUP BY p.organization_id
		)
		SELECT 
			COALESCE(tm.organization_id, tlm.organization_id) as organization_id,
			COALESCE(tm.public_mcp_servers, 0) as public_mcp_servers,
			COALESCE(tm.private_mcp_servers, 0) as private_mcp_servers,
			COALESCE(tm.total_toolsets, 0) as total_toolsets,
			COALESCE(tlm.total_tools, 0) as total_tools
		FROM toolset_metrics tm
		FULL OUTER JOIN tool_metrics tlm ON tm.organization_id = tlm.organization_id
	`

	rows, err := c.db.Query(ctx, query)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to query platform usage metrics", attr.SlogError(err))
		return fmt.Errorf("failed to query platform usage metrics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var metrics PlatformUsageMetrics

		if err := rows.Scan(
			&metrics.OrganizationID,
			&metrics.PublicMCPServers,
			&metrics.PrivateMCPServers,
			&metrics.TotalToolsets,
			&metrics.TotalTools,
		); err != nil {
			c.logger.ErrorContext(ctx, "failed to scan platform usage metrics row", attr.SlogError(err))
			return fmt.Errorf("failed to scan platform usage metrics row: %w", err)
		}

		go c.usageClient.TrackPlatformUsage(context.Background(), usage.PlatformUsageEvent{
			OrganizationID:    metrics.OrganizationID,
			PublicMCPServers:  metrics.PublicMCPServers,
			PrivateMCPServers: metrics.PrivateMCPServers,
			TotalToolsets:     metrics.TotalToolsets,
			TotalTools:        metrics.TotalTools,
		})

		c.logger.InfoContext(ctx, "recorded platform usage metrics",
			attr.SlogOrganizationID(metrics.OrganizationID),
			slog.Int64(publicMCPServersKey, metrics.PublicMCPServers),
			slog.Int64(privateMCPServersKey, metrics.PrivateMCPServers),
			slog.Int64(totalToolsetsKey, metrics.TotalToolsets),
			slog.Int64(totalToolsKey, metrics.TotalTools))
	}

	if err := rows.Err(); err != nil {
		c.logger.ErrorContext(ctx, "error iterating over platform usage metrics rows", attr.SlogError(err))
		return fmt.Errorf("error iterating over platform usage metrics rows: %w", err)
	}

	c.logger.InfoContext(ctx, "Platform usage metrics collection completed successfully")
	return nil
}