package remotemcp

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// instrumentMCPToolCall is the OTel instrument name for the per-tool MCP
// call counter emitted by the remote-MCP proxy stack. The `/mcp` endpoint
// publishes the same name from a different meter scope
// (`server/internal/mcp`); OpenTelemetry separates instruments by meter, so
// the two emit independently.
const instrumentMCPToolCall = "mcp.tool.call"

// ProxyMetrics holds the proxy-level OpenTelemetry instruments published by
// the MCP-aware remote-proxy stack. The proxy package's own [proxy.Metrics]
// is intentionally transport-shaped (method and status_class only);
// method-aware dimensions like the per-tool call counter live here so the
// MCP-aware layer can publish them without leaking method-awareness into the
// proxy.
type ProxyMetrics struct {
	mcpToolCallCounter metric.Int64Counter
}

// NewProxyMetrics constructs the proxy metrics object. Instrument creation
// failures log and produce a metrics value with a nil counter so callers can
// record safely without checking for setup errors at every call site.
func NewProxyMetrics(meter metric.Meter, logger *slog.Logger) *ProxyMetrics {
	counter, err := meter.Int64Counter(
		instrumentMCPToolCall,
		metric.WithDescription("MCP tool call"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create mcp tool call counter", attr.SlogError(err))
	}

	return &ProxyMetrics{
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
func (m *ProxyMetrics) RecordMCPToolCall(ctx context.Context, orgID string, mcpURL string, identity proxy.ServerIdentity, toolName string) {
	if m.mcpToolCallCounter == nil {
		return
	}

	labels := []attribute.KeyValue{
		attr.OrganizationID(orgID),
		attr.McpURL(mcpURL),
		attr.RemoteMCPServerID(identity.RemoteMCPServerID),
		attr.ToolName(toolName),
	}
	if identity.McpServerID != "" {
		labels = append(labels, attr.McpServerID(identity.McpServerID))
	}

	m.mcpToolCallCounter.Add(ctx, 1, metric.WithAttributes(labels...))
}
