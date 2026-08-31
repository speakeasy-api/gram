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
// This is the only instrument covering every production inbound MCP path and
// all three
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
// Segment shape alone is not enough, though: several static routes are
// registered directly beside /mcp/{mcpSlug} and occupy the same one-segment
// shape without being MCP endpoints. Those are excluded by name — see
// isSlugSiblingRoute.
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
		return isEndpointSlug(tail) && !isSlugSiblingRoute(tail)
	case "x":
		// /x/mcp/{slug} — toolset-backed, remote-backed, and tunneled. Carries
		// the same OAuth callback siblings as /mcp/ (internal/xmcp/service.go).
		slug, ok := strings.CutPrefix(tail, "mcp/")
		return ok && isEndpointSlug(slug) && !isSlugSiblingRoute(slug)
	case "platform":
		// /platform/mcp/{toolsetSlug}. Nothing static is registered beside it,
		// so every one-segment tail here really is a slug.
		slug, ok := strings.CutPrefix(tail, "mcp/")
		return ok && isEndpointSlug(slug)
	case "platform-mcp":
		// POST /platform-mcp is Gram's own platform MCP server
		// (internal/platformmcp), served by the go-sdk's Streamable HTTP
		// handler. It carries no slug — the bare path is the endpoint — and
		// everything below it (/authorize, /token, /provider-setup) is OAuth or
		// setup machinery, not JSON-RPC. /platform-mcp/local-fixture/mcp is a
		// JSON-RPC endpoint but is mounted only when the local fixture is
		// configured and is bound to loopback, so it is deliberately excluded.
		return tail == ""
	default:
		return false
	}
}

// isEndpointSlug reports whether seg is a single non-empty path segment, the
// shape an MCP endpoint slug occupies. A further slash means a sub-route.
func isEndpointSlug(seg string) bool {
	return seg != "" && !strings.Contains(seg, "/")
}

// isSlugSiblingRoute reports whether seg names a static route registered
// directly under /mcp/ or /x/mcp/ rather than an endpoint slug.
//
// chi resolves a static pattern ahead of a parameterized one, so these paths
// always reach their own handler and never the MCP endpoint handler. This
// middleware runs before routing, however, and sees only the path, where a
// static one-segment route is indistinguishable from a slug by shape alone.
//
// Getting this wrong is not merely a telemetry gap, because MCPSecurity shares
// this predicate. A misclassified OAuth callback gets origin-checked, and the
// browser navigation returning from an upstream IdP carries
// Sec-Fetch-Site: cross-site by construction — it is a redirect arriving from
// the IdP's own origin, with no Origin header, which is exactly the shape
// CrossOriginProtection refuses. That is a 403 in the middle of every
// remote-login and IdP-backed authorization flow, which is what shipping the
// original predicate did to production.
func isSlugSiblingRoute(seg string) bool {
	switch seg {
	// GET /mcp/idp_callback and GET /x/mcp/idp_callback: the upstream IdP
	// redirects the browser here to complete an authorization code exchange.
	case "idp_callback":
		return true
	// GET /mcp/remote_login_callback and GET /x/mcp/remote_login_callback: the
	// same, for the remote-session login flow that writes remote_sessions.
	case "remote_login_callback":
		return true
	}

	// /mcp/install-page-{hash}.js, /mcp/consent-page-{hash}.js and
	// /mcp/consent-tools-{hash}.js are content-hashed browser assets. No slug
	// can collide with them: constants.SlugPattern is ^[a-z0-9_-]{1,128}$,
	// which admits no dot, so the suffix alone settles it and new hashed
	// assets need no update here.
	return strings.HasSuffix(seg, ".js")
}
