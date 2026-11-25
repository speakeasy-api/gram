package dataexports

import (
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
