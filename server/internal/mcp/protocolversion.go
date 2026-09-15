package mcp

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// recordMCPProtocolVersionSpan annotates the active span with the MCP protocol
// revision a client asked for and the one actually in effect, so a trace can
// be attributed to a protocol version and a downgraded negotiation is visible
// rather than implied.
//
// Either value may be empty — a client can omit protocolVersion from its
// initialize params — in which case that attribute is omitted rather than
// recorded as "". Values are client-supplied, so they are bounded by
// [mcpversions.Sanitize] before being recorded; the raw (sanitized) value is
// kept deliberately, since an unrecognized revision is exactly what an
// operator debugging a version-specific failure needs to see. Bucketing to
// [mcpversions.Other] applies only to metric dimensions.
//
// This stamps whichever span is current. On the /mcp and /platform/mcp paths
// that is the otelhttp server span; the remote MCP proxy sets its own
// attributes on its per-request child span instead, so a downgrade there is
// attributed to the upstream leg rather than to Gram.
func recordMCPProtocolVersionSpan(ctx context.Context, requested, negotiated string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	attrs := make([]attribute.KeyValue, 0, 2)
	if v := mcpversions.Sanitize(requested); v != "" {
		attrs = append(attrs, attr.MCPRequestedProtocolVersion(v))
	}
	if v := mcpversions.Sanitize(negotiated); v != "" {
		attrs = append(attrs, attr.MCPNegotiatedProtocolVersion(v))
	}

	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
}
