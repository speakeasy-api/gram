package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// inboundTraceID and inboundSpanID are fake values standing in for a
// client-supplied W3C trace context.
const (
	inboundTraceID = "0123456789abcdef0123456789abcdef"
	inboundSpanID  = "0123456789abcdef"
)

// newInboundRequest builds a request carrying both a client-supplied
// traceparent and baggage header, contextualized to the test.
func newInboundRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("traceparent", "00-"+inboundTraceID+"-"+inboundSpanID+"-01")
	req.Header.Set("baggage", "organization_slug=inbound-org")
	return req.WithContext(t.Context())
}

func TestIsOTelPublicEndpointTrustedRoutesAreNotPublic(t *testing.T) {
	t.Parallel()

	trusted := []string{
		"/rpc/access.listRoles",
		"/rpc/external.receiveWorkOSWebhook",
		"/admin/organizations.list",
		"/admin/auth.callback",
	}

	for _, path := range trusted {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		require.Falsef(t, middleware.IsOTelPublicEndpoint(req), "expected %q to be trusted (not public)", path)
	}
}

func TestIsOTelPublicEndpointPublicRoutesArePublic(t *testing.T) {
	t.Parallel()

	public := []string{
		"/mcp",
		"/mcp/some-slug",
		"/mcp/idp_callback",
		"/oauth/some-slug/token",
		"/oauth-external/callback",
		"/x/mcp/some-slug",
		"/.well-known/oauth-protected-resource/mcp/some-slug",
		"/.well-known/oauth-authorization-server/x/mcp/some-slug",
		"/chat/completions",
		"/openapi.yaml",
	}

	for _, path := range public {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		require.Truef(t, middleware.IsOTelPublicEndpoint(req), "expected %q to be public", path)
	}
}

// TestIsOTelPublicEndpointPrefixBoundaries guards the segment-aware prefix
// match: a trusted prefix must only match a whole path segment, never an
// arbitrary substring, so sibling routes that merely share a prefix stay
// public.
func TestIsOTelPublicEndpointPrefixBoundaries(t *testing.T) {
	t.Parallel()

	public := []string{
		"/rpc",
		"/rpcx",
		"/rpc-internal/foo",
		"/RPC/foo", // matching is case-sensitive
		"/admin",
		"/admins",
		"/admin-tool/foo",
		"/administration",
	}

	for _, path := range public {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		require.Truef(t, middleware.IsOTelPublicEndpoint(req), "expected boundary case %q to be public", path)
	}
}

// TestIsOTelPublicEndpointTraversalIsNotReachable documents that a "../"
// traversal resolving a trusted prefix classifies as trusted at this raw-path
// predicate. That is safe only because chi matches path segments literally and
// 404s such a request before any handler runs.
func TestIsOTelPublicEndpointTraversalIsNotReachable(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/rpc/../mcp/some-slug", nil)
	require.False(t, middleware.IsOTelPublicEndpoint(req), "a /rpc/.. traversal is classified trusted by the raw-path predicate; chi 404s it before any handler runs")
}

// TestIsOTelPublicEndpointKnownRouteSurface is the safe-by-default census
// guardrail. It is a hand-maintained snapshot of the server's top-level route
// prefixes paired with the trust classification IsOTelPublicEndpoint applies to
// each; it is not derived from the live mux, so it documents intent and forces a
// deliberate decision in review rather than automatically detecting a new route.
// /healthz and the marketplace /m/ and /p/ routes are intentionally absent:
// they are short-circuited ahead of the otelhttp middleware and never reach the
// predicate.
//
// When you add a new top-level route prefix, add it here with a deliberate
// public/trusted decision. The unknown-future-route case asserts the
// safe-by-default behavior: anything not on the trusted allowlist is public, so
// a forgotten route degrades safely (a fresh root span) rather than silently
// trusting potentially untrusted trace context.
func TestIsOTelPublicEndpointKnownRouteSurface(t *testing.T) {
	t.Parallel()

	census := map[string]bool{ // path -> want public
		// Trusted first-party surfaces.
		"/rpc/toolsets.list":        false,
		"/admin/organizations.list": false,
		// Public untrusted surfaces.
		"/mcp/some-slug":                                        true,
		"/x/mcp/some-slug":                                      true,
		"/oauth/some-slug/authorize":                            true,
		"/oauth-external/authorize":                             true,
		"/.well-known/oauth-protected-resource/mcp/some-slug":   true,
		"/.well-known/oauth-authorization-server/mcp/some-slug": true,
		"/chat/completions":                                     true,
		"/openapi.yaml":                                         true,
		// Safe-by-default: an unclassified future route is public.
		"/some-future-route": true,
	}

	for path, wantPublic := range census {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		require.Equalf(t, wantPublic, middleware.IsOTelPublicEndpoint(req), "unexpected classification for %q", path)
	}
}

func TestDropInboundOTelBaggageRemovesHeaderOnPublicRoute(t *testing.T) {
	t.Parallel()

	var got *http.Request
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r })

	middleware.DropInboundOTelBaggage(inner).ServeHTTP(httptest.NewRecorder(), newInboundRequest(t, http.MethodPost, "/mcp/some-slug"))

	require.Empty(t, got.Header.Get("baggage"), "baggage header must be removed on public routes")
	require.NotEmpty(t, got.Header.Get("traceparent"), "traceparent must be preserved for span linking")
}

func TestDropInboundOTelBaggageKeepsHeaderOnTrustedRoute(t *testing.T) {
	t.Parallel()

	var got *http.Request
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r })

	middleware.DropInboundOTelBaggage(inner).ServeHTTP(httptest.NewRecorder(), newInboundRequest(t, http.MethodPost, "/rpc/toolsets.list"))

	require.Equal(t, "organization_slug=inbound-org", got.Header.Get("baggage"), "baggage header must be preserved on trusted routes")
}

// TestOTelPublicEndpointPublicRouteNewRootsLinksAndDropsBaggage exercises the
// full chain end to end: a public route discards the inbound traceparent as
// parent, starts a fresh root span (a new trace id), records the inbound
// context as a span link, and keeps the dropped inbound baggage out of the
// request context.
func TestOTelPublicEndpointPublicRouteNewRootsLinksAndDropsBaggage(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	var gotBaggage baggage.Baggage
	chain := newOTelChain(t, recorder, &gotBaggage)

	chain.ServeHTTP(httptest.NewRecorder(), newInboundRequest(t, http.MethodPost, "/mcp/some-slug"))

	require.Equal(t, 0, gotBaggage.Len(), "inbound baggage must not reach the request context on public routes")

	wantTrace, err := trace.TraceIDFromHex(inboundTraceID)
	require.NoError(t, err)
	wantSpan, err := trace.SpanIDFromHex(inboundSpanID)
	require.NoError(t, err)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	require.False(t, span.Parent().IsValid(), "public route span must not adopt the inbound context as parent")
	require.NotEqual(t, wantTrace, span.SpanContext().TraceID(), "public route span must start a fresh trace id")
	require.Len(t, span.Links(), 1, "public route span must link the inbound context")
	require.Equal(t, wantTrace, span.Links()[0].SpanContext.TraceID())
	require.Equal(t, wantSpan, span.Links()[0].SpanContext.SpanID())
}

// TestOTelPublicEndpointTrustedRouteKeepsParentAndBaggage verifies the
// complementary case: a trusted route continues the inbound trace as
// parent-child, adds no link, and preserves the inbound baggage in the request
// context.
func TestOTelPublicEndpointTrustedRouteKeepsParentAndBaggage(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	var gotBaggage baggage.Baggage
	chain := newOTelChain(t, recorder, &gotBaggage)

	chain.ServeHTTP(httptest.NewRecorder(), newInboundRequest(t, http.MethodPost, "/rpc/toolsets.list"))

	require.Equal(t, "inbound-org", gotBaggage.Member("organization_slug").Value(), "inbound baggage must be preserved on trusted routes")

	wantTrace, err := trace.TraceIDFromHex(inboundTraceID)
	require.NoError(t, err)
	wantSpan, err := trace.SpanIDFromHex(inboundSpanID)
	require.NoError(t, err)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	require.True(t, span.Parent().IsValid(), "trusted route span must keep the inbound parent")
	require.Equal(t, wantTrace, span.Parent().TraceID())
	require.Equal(t, wantSpan, span.Parent().SpanID())
	require.Equal(t, wantTrace, span.SpanContext().TraceID(), "trusted route span must continue the inbound trace")
	require.Empty(t, span.Links(), "trusted route span must not link the inbound context")
}

// newOTelChain wires DropInboundOTelBaggage in front of an otelhttp handler
// configured with IsOTelPublicEndpoint, mirroring the server middleware order.
// The inner handler captures the baggage visible to downstream handlers.
func newOTelChain(t *testing.T, recorder *tracetest.SpanRecorder, gotBaggage *baggage.Baggage) http.Handler {
	t.Helper()

	// testenv.NewTracerProvider(t) returns a no-op provider, so it cannot be
	// used here: these tests need a real SDK provider with a SpanRecorder to
	// inspect the emitted span's parent and links.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	propagators := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotBaggage = baggage.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	return middleware.DropInboundOTelBaggage(otelhttp.NewHandler(
		inner,
		"http",
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithPropagators(propagators),
		otelhttp.WithPublicEndpointFn(middleware.IsOTelPublicEndpoint),
	))
}
