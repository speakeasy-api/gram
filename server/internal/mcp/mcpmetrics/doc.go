// Package mcpmetrics owns every OpenTelemetry instrument the MCP runtime
// publishes: the per-request census counter, the handshake counter, the
// tool-call counter, the request-duration histogram, and the user-facing
// OAuth flow counters.
//
// It is a leaf package for the same reason as its sibling mcprequests: the
// remote MCP proxy stack (server/internal/remotemcp) publishes the
// per-request census from its own meter and cannot import the mcp service
// package. [Metrics] is the mcp service's full instrument set; [RequestCounter]
// stands alone so remotemcp can construct just the census without minting the
// service-only instruments on its meter.
package mcpmetrics
