// Package mcpoutbound prepares contexts for outbound MCP client connections
// made while serving an inbound MCP request.
package mcpoutbound

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// DetachContext returns a context for an outbound MCP client that keeps the
// caller's cancellation and trace span but none of its other values.
//
// The go-sdk streamable server stores the inbound request's
// Mcp-Protocol-Version in the request context, and its client sends a version
// found under the same key in place of the one it negotiated with the
// upstream. A client connected from inside a stateless MCP tool call would
// otherwise advertise the caller's protocol revision to an upstream that never
// agreed to it.
func DetachContext(ctx context.Context) (context.Context, context.CancelFunc) {
	detached, cancel := context.WithCancel(trace.ContextWithSpan(context.Background(), trace.SpanFromContext(ctx)))
	stop := context.AfterFunc(ctx, cancel)
	return detached, func() {
		stop()
		cancel()
	}
}
