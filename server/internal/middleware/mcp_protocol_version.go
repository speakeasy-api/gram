package middleware

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// MCPProtocolVersionTelemetry lifts the MCP-Protocol-Version header onto the
// server span for requests to an MCP JSON-RPC endpoint, so a request that
// carries one can be attributed to a protocol revision in traces.
//
// This is the only instrument covering every inbound MCP path and all three
// Streamable HTTP verbs from a single place, but it sees only what the header
// carries, which varies by revision. Clients on revisions before 2025-06-18 do
// not send it at all. Clients that do send it omit it from an `initialize`
// request, where no version is established yet; the initialize handlers record
// what those clients asked for instead. Clients on 2026-07-28 have no
// `initialize` to omit it from and declare a version on every request, so the
// header is the only source for them.
//
// Must be registered after otelhttp so the span is in the request context.
// Header values are client-supplied input and are sanitized before becoming
// attributes; the raw (bounded) value is kept because seeing what a
// non-conforming client actually sent is the diagnostic point. Non-MCP routes
// are untouched.
//
// Gram is not the only reader: the remote MCP proxy forwards this header
// upstream untouched, so nothing here may mutate it.
func MCPProtocolVersionTelemetry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMCPJSONRPCEndpoint(r.URL.Path) {
			if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
				if v := mcpversions.Sanitize(r.Header.Get(mcpversions.HTTPHeader)); v != "" {
					span.SetAttributes(attr.MCPNegotiatedProtocolVersion(v))
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// isMCPJSONRPCEndpoint reports whether path addresses one of the MCP JSON-RPC
// endpoints, as distinct from the OAuth, metadata, and install-page routes
// that share the /mcp/ prefix.
//
// Matching is on segment shape rather than a bare prefix because
// /mcp/{slug}/token, /register, /authorize, and /connect are not MCP endpoints
// and never carry a protocol version. Route patterns are not available here:
// this middleware is registered via goa's Muxer.Use, which delegates to chi's
// Router.Use and therefore runs before routing.
//
// The shapes matched here are the MCP JSON-RPC routes the server registers. A
// new MCP route that does not match one of them is served uninstrumented, with
// no failure to signal it.
func isMCPJSONRPCEndpoint(path string) bool {
	rest, ok := strings.CutPrefix(path, "/")
	if !ok {
		return false
	}

	switch head, tail, _ := strings.Cut(rest, "/"); head {
	case "mcp":
		// /mcp/{mcpSlug} — the hosted toolset endpoint. A further slash means
		// an OAuth or metadata sub-route, not the MCP endpoint itself.
		return tail != "" && !strings.Contains(tail, "/")
	case "x", "platform":
		// /x/mcp/{slug} (toolset-backed, remote-backed, and tunneled) and
		// /platform/mcp/{toolsetSlug}.
		slug, ok := strings.CutPrefix(tail, "mcp/")
		return ok && slug != "" && !strings.Contains(slug, "/")
	case "platform-mcp":
		// POST /platform-mcp is Gram's own platform MCP server
		// (internal/platformmcp), served by the go-sdk's Streamable HTTP
		// handler. It carries no slug — the bare path is the endpoint — and
		// everything below it (/authorize, /token, /provider-setup,
		// /local-fixture/*) is OAuth or setup machinery, not JSON-RPC.
		return tail == ""
	default:
		return false
	}
}
