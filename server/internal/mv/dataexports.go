package mv

import (
	"sort"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
)

func BuildOtelDestinationView(row repo.OtelDestination, headers map[string]string, sensitiveData string) *gen.OtelDestination {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	headerViews := make([]*gen.OtelDestinationHeader, 0, len(names))
	for _, name := range names {
		headerViews = append(headerViews, &gen.OtelDestinationHeader{
			Name:     name,
			HasValue: headers[name] != "",
		})
	}

	return &gen.OtelDestination{
		ID:            row.ID.String(),
		ProjectID:     row.ProjectID.String(),
		EndpointURL:   row.EndpointUrl,
		SensitiveData: sensitiveData,
		Headers:       headerViews,
		CreatedAt:     conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:     conv.FromPGTimestamptz(row.UpdatedAt),
	}
}

func BuildDataExportRouteView(row repo.DataExportRoute) *gen.DataExportRoute {

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
