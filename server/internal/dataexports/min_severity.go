package dataexports

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	MinSeverityInfo  = "info"
	MinSeverityWarn  = "warn"
	MinSeverityError = "error"

	// DefaultMCPConnectionLogMinSeverity applies to an MCP connection logs route with no settings row.
	// It must match the mcp_connection_log_routes.min_severity column default.
	DefaultMCPConnectionLogMinSeverity = MinSeverityError
)

// NormalizeMinSeverity validates and returns a supported minimum level.
func NormalizeMinSeverity(value string) (string, error) {
	switch value {
	case MinSeverityInfo, MinSeverityWarn, MinSeverityError:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported minimum level %q", value)
	}
}

// RouteMinSeverity is a route's minimum level from a stored settings value. It is empty for
// data sources without one, and the default when an MCP connection logs route has no row.
func RouteMinSeverity(dataSource string, stored pgtype.Text) string {
	if dataSource != DataSourceMCPConnectionLogs {
		return ""
	}
	if stored.Valid {
		return stored.String
	}
	return DefaultMCPConnectionLogMinSeverity
}
