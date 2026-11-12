package dataexports

import (
	"fmt"
	"slices"
)

const (
	DataSourceProductTelemetry = "product_telemetry"
	DataSourceRiskFindings     = "risk_findings"
	// DataSourceToolCallLogs exports the tool call records Speakeasy writes itself
	// when it executes a tool — the rows behind the Tool Logs pages. Product
	// telemetry cannot carry them: it relays what reached the OTLP ingest
	// endpoints, and a tool Speakeasy runs never passes through those.
	DataSourceToolCallLogs = "tool_call_logs"
	// DataSourceMCPConnectionLogs exports failed MCP connection attempts.
	DataSourceMCPConnectionLogs = "mcp_connection_logs"
)

// Signal is one OTLP signal a data source exports.
type Signal string

const (
	SignalLogs    Signal = "logs"
	SignalTraces  Signal = "traces"
	SignalMetrics Signal = "metrics"
)

// Source is one data export source and the OTLP signals it sends.
type Source struct {
	Name    string
	Signals []Signal
}

// sources is the registry of data export sources. A new source needs a test
// event for every signal it lists, or the route tester refuses to start.
var sources = []Source{
	{Name: DataSourceProductTelemetry, Signals: []Signal{SignalLogs, SignalTraces, SignalMetrics}},
	{Name: DataSourceRiskFindings, Signals: []Signal{SignalLogs}},
	{Name: DataSourceToolCallLogs, Signals: []Signal{SignalLogs}},
	{Name: DataSourceMCPConnectionLogs, Signals: []Signal{SignalLogs}},
}

// Sources returns a copy of the registry.
func Sources() []Source {
	out := make([]Source, len(sources))
	for i, source := range sources {
		out[i] = Source{Name: source.Name, Signals: slices.Clone(source.Signals)}
	}
	return out
}

// LookupSource returns the registered source with this name.
func LookupSource(name string) (Source, bool) {
	for _, source := range sources {
		if source.Name == name {
			return Source{Name: source.Name, Signals: slices.Clone(source.Signals)}, true
		}
	}
	return Source{Name: "", Signals: nil}, false
}

// NormalizeDataSource validates and returns a supported data export source.
func NormalizeDataSource(value string) (string, error) {
	if _, ok := LookupSource(value); !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return value, nil
}
