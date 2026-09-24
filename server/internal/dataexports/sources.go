package dataexports

import "fmt"

const (
	DataSourceProductTelemetry = "product_telemetry"
	DataSourceRiskFindings     = "risk_findings"
	// DataSourceToolCallLogs exports the tool call records Gram writes itself
	// when it executes a tool — the rows behind the Tool Logs pages. Product
	// telemetry cannot carry them: it relays what reached the OTLP ingest
	// endpoints, and a tool Gram runs never passes through those.
	DataSourceToolCallLogs = "tool_call_logs"
)

var validDataSources = map[string]struct{}{
	DataSourceProductTelemetry: {},
	DataSourceRiskFindings:     {},
	DataSourceToolCallLogs:     {},
}

// NormalizeDataSource validates and returns a supported data export source.
func NormalizeDataSource(value string) (string, error) {
	if _, ok := validDataSources[value]; !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return value, nil
}
