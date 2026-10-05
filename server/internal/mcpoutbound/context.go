// Package mcpoutbound prepares contexts for outbound MCP client connections
// made while serving an inbound MCP request.
package mcpoutbound

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/trace"
)

// DetachContext returns a context for an outbound MCP client that keeps the
// caller's deadline, cancellation and trace span but none of its other values.
//
// The go-sdk streamable server stores the inbound request's
// Mcp-Protocol-Version in the request context, and its client sends a version
// found under the same key in place of the one it negotiated with the
// upstream. A client connected from inside a stateless MCP tool call would
// otherwise advertise the caller's protocol revision to an upstream that never
// agreed to it.
func DetachContext(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := trace.ContextWithSpan(context.Background(), trace.SpanFromContext(ctx))
	cancelDeadline := context.CancelFunc(func() {})
	if deadline, ok := ctx.Deadline(); ok {
		// Copying the deadline keeps a caller's timeout reporting
		// context.DeadlineExceeded rather than context.Canceled.
		detached, cancelDeadline = context.WithDeadline(detached, deadline)
	}
	detached, cancel := context.WithCancel(detached)
	stop := context.AfterFunc(ctx, func() {
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			cancel()
		}
	})
	return detached, func() {
		stop()
		cancel()
		cancelDeadline()
	}
}
