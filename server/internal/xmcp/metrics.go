package xmcp

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// instrumentMCPToolCall is the OTel instrument name for the per-tool MCP
// call counter. The `/mcp` endpoint publishes the same name from a different
// meter scope (`server/internal/mcp`); OpenTelemetry separates instruments by
// meter, so the two emit independently.
const instrumentMCPToolCall = "mcp.tool.call"

// metrics holds the xmcp-level OpenTelemetry instruments. The proxy package's
// own [proxy.Metrics] is intentionally transport-shaped (method and
// status_class only); method-aware dimensions like the per-tool call counter
// live here so xmcp can publish them without leaking method-awareness into
// the proxy.
type metrics struct {
	mcpToolCallCounter metric.Int64Counter
}

// newMetrics constructs the xmcp metrics object. Instrument creation failures
// log and produce a metrics value with a nil counter so callers can record
// safely without checking for setup errors at every call site.
func newMetrics(meter metric.Meter, logger *slog.Logger) *metrics {
	counter, err := meter.Int64Counter(
		instrumentMCPToolCall,
		metric.WithDescription("MCP tool call"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp tool call counter", attr.SlogError(err))
	}

	return &metrics{
		mcpToolCallCounter: counter,
	}
}

// RecordMCPToolCall increments the per-tool MCP call counter with the given
// dimensions. Safe to call on a metrics value whose counter failed to
// initialize — the call is a no-op in that case.
//
// The counter is an attempt counter, not a success counter: the registered
// interceptor records on every observed tools/call request, including those
// later rejected by free-tier usage limits or per-tool RBAC. This mirrors
// `/mcp`'s `RecordMCPToolCall` placement at `rpc_tools_call.go:124`, which
// also fires before the per-tool authz check. There is no `result` or
// `outcome` attribute today; consumers that need to distinguish accepted
// from rejected attempts should aggregate this counter alongside the
// proxy-level request status histogram in [proxy.Metrics].
func (m *metrics) RecordMCPToolCall(ctx context.Context, orgID string, mcpURL string, toolName string) {
	if m.mcpToolCallCounter == nil {
		return
	}

	m.mcpToolCallCounter.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(orgID),
		attr.McpURL(mcpURL),
		attr.ToolName(toolName),
	))
}
