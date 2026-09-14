package mcpmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// InstrumentMCPRequest is the OTel instrument name for the per-request MCP
// census counter. Both the hosted dispatch meter (server/internal/mcp) and
// the remote proxy meter (server/internal/remotemcp) publish it;
// OpenTelemetry separates instruments by meter, so the two emit independently
// and aggregate by name — the same arrangement `mcp.tool.call` already uses.
const InstrumentMCPRequest = "mcp.request"

// RequestCounter owns the census counter. A nil *RequestCounter is valid —
// Record becomes a no-op — so tests and callers that do not care about
// metrics can pass nil.
type RequestCounter struct {
	requests metric.Int64Counter
}

// NewRequestCounter constructs the census counter. An instrument creation
// failure is logged and leaves the instrument nil; Record handles nil
// instruments so partial construction still produces a usable value.
func NewRequestCounter(meter metric.Meter, logger *slog.Logger) *RequestCounter {
	requests, err := meter.Int64Counter(
		InstrumentMCPRequest,
		metric.WithDescription("MCP JSON-RPC requests observed at dispatch, by protocol revision, method, serving surface, and public/private network surface"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(InstrumentMCPRequest), attr.SlogError(err))
	}

	return &RequestCounter{requests: requests}
}

// Record counts one dispatched MCP request, dimensioned by clamped protocol
// revision, clamped method, and surface.
//
// This is the unsampled census that survives the 2026-07-28 removal of
// `initialize`: it keys on the request rather than on a session-opening
// event, so every protocol era is counted on every request. It is recorded at
// the JSON-RPC dispatch sites and in the remote proxy's request interceptor —
// after routing, authentication, and body parsing — so scanner probes, CORS
// preflights, and requests rejected before dispatch are deliberately not
// counted, matching the semantics of the `mcp.initialize` handshake counter
// it complements.
//
// Like `mcp.initialize`, it deliberately carries no per-server dimension:
// traces are sampled and cannot be counted, and the census question ("how
// many requests still arrive from revision X on surface Y?") needs
// fleet-wide aggregation with a bounded series count. It also does not share
// totals with the `mcp.request.duration` histogram, which carries a
// per-server URL dimension and is recorded only on the hosted and platform
// dispatch paths — the two instruments differ by the remote/tunnel proxy
// traffic this counter additionally covers.
//
// protocolVersion is the version the request declared, with
// negotiated-version semantics throughout: the MCP-Protocol-Version header
// where present, otherwise the 2026-07-28 per-request `_meta` key that header
// mirrors. The legacy `initialize` body's requested version is deliberately
// NOT an input — it carries different (pre-negotiation) semantics, and
// `mcp.initialize` already counts handshakes by requested × negotiated
// revision. Empty clamps to "none": on `initialize` rows that is definitional
// (the header is never sent there), and on other methods it identifies the
// pre-2025-06-18 cohort.
func (c *RequestCounter) Record(ctx context.Context, protocolVersion, method string, surface Surface) {
	if c == nil || c.requests == nil {
		return
	}

	c.requests.Add(ctx, 1, metric.WithAttributes(
		attr.MCPNegotiatedProtocolVersion(mcpversions.Clamp(mcpversions.Sanitize(protocolVersion))),
		attr.McpMethod(mcprequests.ClampMethod(method)),
		attr.McpSurface(string(surface)),
		attr.NetworkSurface(NetworkSurfaceFromContext(ctx)),
	))
}
