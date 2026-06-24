package middleware

import (
	"net/http"
	"strings"
)

// trustedOTelRoutePrefixes enumerates the path prefixes whose inbound W3C trace
// context is trusted: gram's first-party RPC surface over /rpc and the staff
// admin UI over /admin. For requests under them otelhttp keeps the inbound
// traceparent as the span parent (preserving Datadog RUM->backend and internal
// service-to-service trace continuity) and their inbound baggage is left intact.
//
// A few /rpc endpoints are also reachable by third parties (signed webhooks,
// unauthenticated auth callbacks, API-key hook ingestion). They are accepted as
// trusted here to keep the trusted set small and fail-safe. Every other route
// is treated as an OpenTelemetry "public endpoint". For OTel public endpoints,
// the traceparent, tracestate, and baggage are unauthenticated and treated as
// untrusted input. For those routes otelhttp starts a fresh root span (a new
// trace id) and records the inbound SpanContext as a span link instead of
// adopting it as the parent.
//
// This is an inverted, safe-by-default allowlist: a newly added route is
// treated as public unless it is deliberately added here. /healthz and the
// marketplace /m/ and /p/ routes are short-circuited ahead of the otelhttp
// middleware and never reach this predicate. Keep this list and its
// corresponding census test (TestIsOTelPublicEndpointKnownRouteSurface) in sync
// when adding a top-level route prefix.
var trustedOTelRoutePrefixes = []string{"/rpc/", "/admin/"}

// IsOTelPublicEndpoint reports whether a request targets a public
// (untrusted-trace) endpoint. It powers otelhttp's WithPublicEndpointFn and
// gates DropInboundOTelBaggage so the span-link and baggage-drop behaviors
// apply to exactly the same routes.
//
// Matching is on the raw request path because chi route parameters are not yet
// parsed at the otelhttp middleware layer. A path that reaches a trusted prefix
// only through traversal (e.g. "/rpc/../mcp/x") is classified trusted here, but
// chi matches path segments literally and does not clean "..", so such a request
// 404s before reaching any handler and cannot smuggle a public route into the
// trusted set.
func IsOTelPublicEndpoint(r *http.Request) bool {
	path := r.URL.Path
	for _, prefix := range trustedOTelRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// DropInboundOTelBaggage removes the client-supplied W3C `baggage` request
// header on public routes so it is never extracted into the request context.
// Inbound baggage has no integrity protection, so on the public surface it is
// potentially untrusted input that would otherwise flow into metric dimensions
// (the tool-call counter's organization_slug) and propagate to downstream
// services.
func DropInboundOTelBaggage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsOTelPublicEndpoint(r) {
			r.Header.Del("baggage")
		}
		next.ServeHTTP(w, r)
	})
}
