// Package mcprequests carries the per-request MCP metadata primitives shared
// by every inbound MCP surface: the decode of the per-request metadata that
// the 2026-07-28 protocol revision attaches to every request, the declared
// protocol-version precedence, the clamped method dimension, and the field
// sanitization they share. The instruments these feed live in the sibling
// mcpmetrics package; this package holds no OpenTelemetry state.
//
// It is a leaf package by design. The hosted dispatch (server/internal/mcp)
// and the remote MCP proxy stack (server/internal/remotemcp) must read
// request metadata identically, and remotemcp cannot import the mcp service
// package. Anything added here must stay importable by both.
//
// Types prefixed Sanitized hold client-supplied values that have already been
// bounded (control characters dropped, lengths capped), so consumers may
// store or record them as-is. Anything else read off the wire is unsanitized
// until it passes through [SanitizeClientInfoField] or a clamp.
package mcprequests
