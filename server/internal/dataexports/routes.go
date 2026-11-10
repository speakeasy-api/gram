package dataexports

import (
	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
)

// buildDataExportRouteView takes the route's minimum level, which only MCP connection logs routes have.
func buildDataExportRouteView(row repo.DataExportRoute, minSeverity *string) *gen.DataExportRoute {
	return &gen.DataExportRoute{
		ID:                row.ID.String(),
		ProjectID:         row.ProjectID.String(),
		DataSource:        row.DataSource,
		Enabled:           row.Enabled,
		OtelDestinationID: conv.FromNullableUUID(row.OtelDestinationID),
		MinSeverity:       minSeverity,
		CreatedAt:         conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:         conv.FromPGTimestamptz(row.UpdatedAt),
	}
}

// routeMinSeverity looks a route's level up in levels, falling back to the default.
func routeMinSeverity(row repo.DataExportRoute, levels map[uuid.UUID]string) *string {
	if row.DataSource != DataSourceMCPConnectionLogs {
		return nil
	}
	if level, ok := levels[row.ID]; ok {
		return &level
	}
	level := DefaultMCPConnectionLogMinSeverity
	return &level
}
