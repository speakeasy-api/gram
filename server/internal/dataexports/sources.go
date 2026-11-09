package dataexports

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

const (
	DataSourceProductTelemetry = "product_telemetry"
	DataSourceRiskFindings     = "risk_findings"
	// DataSourceToolCallLogs exports the tool call records Speakeasy writes itself
	// when it executes a tool — the rows behind the Tool Logs pages. Product
	// telemetry cannot carry them: it relays what reached the OTLP ingest
	// endpoints, and a tool Speakeasy runs never passes through those.
	DataSourceToolCallLogs = "tool_call_logs"
	// DataSourceMCPConnectionLogs exports failed, blocked and declined MCP connection attempts.
	DataSourceMCPConnectionLogs = "mcp_connection_logs"
)

var validDataSources = map[string]struct{}{
	DataSourceProductTelemetry:  {},
	DataSourceRiskFindings:      {},
	DataSourceToolCallLogs:      {},
	DataSourceMCPConnectionLogs: {},
}

// NormalizeDataSource validates and returns a supported data export source.
func NormalizeDataSource(value string) (string, error) {
	if _, ok := validDataSources[value]; !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return value, nil
}

// DataSourceAvailable reports whether an organization can add a route for a data
// source. It fails closed. Routes that already exist are not checked.
func DataSourceAvailable(ctx context.Context, flags feature.Provider, organizationID, dataSource string) (bool, error) {
	if dataSource != DataSourceMCPConnectionLogs {
		return true, nil
	}
	evaluation, err := feature.EvaluateFlag(ctx, flags, feature.FlagMCPConnectionLogExport, organizationID, nil)
	if err != nil {
		return false, fmt.Errorf("evaluate %s availability: %w", dataSource, err)
	}
	return evaluation == feature.EvaluationEnabled, nil
}
