package dataexports

import (
	"fmt"
	"strings"

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

// NormalizeMinSeverity validates a requested minimum level for a route's data source and returns it trimmed and lowercased.
func NormalizeMinSeverity(dataSource, value string) (string, error) {
	if dataSource != DataSourceMCPConnectionLogs {
		return "", fmt.Errorf("min_severity is only supported on mcp_connection_logs routes, not %s", dataSource)
	}
	level := strings.ToLower(strings.TrimSpace(value))
	switch level {
	case MinSeverityInfo, MinSeverityWarn, MinSeverityError:
		return level, nil
	default:
		return "", fmt.Errorf("min_severity must be info, warn or error, not %q", value)
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
