package mv

import (
	"sort"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
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
		CreatedAt:     row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func BuildDataExportRouteView(row repo.DataExportRoute) *gen.DataExportRoute {
	var destinationID *string
	if row.OtelDestinationID.Valid {
		value := row.OtelDestinationID.UUID.String()
		destinationID = &value
	}

	return &gen.DataExportRoute{
		ID:                row.ID.String(),
		ProjectID:         row.ProjectID.String(),
		DataSource:        row.DataSource,
		Enabled:           row.Enabled,
		OtelDestinationID: destinationID,
		CreatedAt:         row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
