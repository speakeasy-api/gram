package dataexports

import (
	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
)

func buildDataExportRouteView(row repo.DataExportRoute) *gen.DataExportRoute {
	return &gen.DataExportRoute{
		ID:                row.ID.String(),
		ProjectID:         row.ProjectID.String(),
		DataSource:        row.DataSource,
		Enabled:           row.Enabled,
		OtelDestinationID: conv.FromNullableUUID(row.OtelDestinationID),
		CreatedAt:         conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:         conv.FromPGTimestamptz(row.UpdatedAt),
	}
}
