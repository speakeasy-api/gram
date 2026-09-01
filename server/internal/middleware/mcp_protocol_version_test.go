package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// recordSpanForRequest runs the middleware inside a recorded span, mimicking
// otelhttp having already opened the server span, and returns the attributes
// left on that span.
func recordSpanForRequest(t *testing.T, method, path, headerValue string) map[string]string {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	handler := middleware.MCPProtocolVersionTelemetry(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, path, nil)
	if headerValue != "" {
		req.Header.Set("MCP-Protocol-Version", headerValue)
	}

	ctx, span := provider.Tracer("test").Start(t.Context(), "http")
	handler.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	span.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)

	got := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	return got
}

func TestMCPProtocolVersionTelemetryRecordsHeaderOnHostedEndpoint(t *testing.T) {
	t.Parallel()

	got := recordSpanForRequest(t, http.MethodPost, "/mcp/my-server", mcpversions.Version20250618)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

func TestMCPProtocolVersionTelemetryRecordsHeaderOnXMCPEndpoint(t *testing.T) {
	t.Parallel()

	got := recordSpanForRequest(t, http.MethodPost, "/x/mcp/my-server", mcpversions.Version20260728)
	require.Equal(t, mcpversions.Version20260728, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

func TestMCPProtocolVersionTelemetryRecordsHeaderOnPlatformEndpoint(t *testing.T) {
	t.Parallel()

	got := recordSpanForRequest(t, http.MethodPost, "/platform/mcp/my-toolset", mcpversions.Version20250326)
	require.Equal(t, mcpversions.Version20250326, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

func TestMCPProtocolVersionTelemetryCoversGetAndDelete(t *testing.T) {
	t.Parallel()

	// Streamable HTTP uses GET to open an SSE stream and DELETE to terminate a
	// session; both carry the header and both reach the remote MCP proxy.
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		got := recordSpanForRequest(t, method, "/x/mcp/my-server", mcpversions.Version20250618)
		require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)], "method %s", method)
	}
}

// routePathForSlug turns a chi route pattern into a concrete request path by
// substituting a slug for its single parameter.
func routePathForSlug(t *testing.T, pattern, slug string) string {
	t.Helper()

	open := strings.IndexByte(pattern, '{')
	closing := strings.IndexByte(pattern, '}')
	require.Greater(t, closing, open, "pattern %q has no route parameter", pattern)
	require.GreaterOrEqual(t, open, 0, "pattern %q has no route parameter", pattern)

	return pattern[:open] + slug + pattern[closing+1:]
}

// TestMCPProtocolVersionTelemetryMatchesRegisteredRoutes derives paths from the
// route patterns the server actually registers rather than from string literals
// repeated here. The middleware duplicates route knowledge outside the router,
// so a route that moves out from under it would otherwise be served
// uninstrumented with nothing to signal it.
//
// Every MCP endpoint mounts all three Streamable HTTP verbs, and the protocol
// version is carried on all of them: POST for JSON-RPC messages, GET for the
// standalone SSE stream, DELETE for session termination. Asserting the full
// cross-product keeps a verb-specific gap from hiding behind a passing POST.
func TestMCPProtocolVersionTelemetryMatchesRegisteredRoutes(t *testing.T) {
	t.Parallel()

	patterns := []string{mcp.PublicServerRoute, mcp.PlatformToolsetRoute, xmcp.RuntimePath}
	methods := []string{http.MethodPost, http.MethodGet, http.MethodDelete}

	for _, pattern := range patterns {
		path := routePathForSlug(t, pattern, "my-server")
		for _, method := range methods {
			got := recordSpanForRequest(t, method, path, mcpversions.Version20250618)
			require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)],
				"route %q resolved to %q, which the middleware does not match for %s", pattern, path, method)
		}
	}

	// Gram's own platform MCP server carries no slug, so routePathForSlug
	// cannot derive it, and the constant cannot be imported: internal/platformmcp
	// imports this package, so referencing platformmcp.Path here would be an
	// import cycle. Keep this literal in lockstep with it. Registered for POST
	// only (see cmd/gram/platform_mcp.go), hence no verb cross-product.
	got := recordSpanForRequest(t, http.MethodPost, "/platform-mcp", mcpversions.Version20250618)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)],
		"/platform-mcp is an MCP JSON-RPC endpoint and must be matched")
}

func TestMCPProtocolVersionTelemetryIgnoresOAuthSubRoutes(t *testing.T) {
	t.Parallel()

	// These share the /mcp/ prefix but are not MCP endpoints and never carry a
	// protocol version.
	for _, path := range []string{
		"/mcp/my-server/token",
		"/mcp/my-server/register",
		"/mcp/my-server/authorize",
		"/mcp/my-server/connect",
		"/mcp/my-server/install",
		"/platform-mcp/authorize",
		"/platform-mcp/token",
		"/platform-mcp/provider-setup",
		"/platform-mcp/local-fixture/mcp",
	} {
		got := recordSpanForRequest(t, http.MethodPost, path, mcpversions.Version20250618)
		require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey), "path %s", path)
	}
}

// TestMCPProtocolVersionTelemetryIgnoresSlugSiblingRoutes is the inverse of
// TestMCPProtocolVersionTelemetryMatchesRegisteredRoutes: these static routes
// are registered directly beside /mcp/{mcpSlug} and /x/mcp/{mcpSlug}, so they
// occupy the slug position without being MCP endpoints and chi resolves them
// first. Shape alone cannot tell them apart from a slug, so they are excluded
// by name.
//
// Keep this list in lockstep with the one-segment routes registered in
// internal/mcp/impl.go and internal/xmcp/service.go. MCPSecurity shares this
// predicate, so a route that regresses back into it is answered with 403
// rather than merely losing an attribute.
func TestMCPProtocolVersionTelemetryIgnoresSlugSiblingRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/mcp/idp_callback",
		"/mcp/remote_login_callback",
		"/mcp/install-page-9f86d081.js",
		"/mcp/consent-page-9f86d081.js",
		"/mcp/consent-tools-9f86d081.js",
		"/x/mcp/idp_callback",
		"/x/mcp/remote_login_callback",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			got := recordSpanForRequest(t, method, path, mcpversions.Version20250618)
			require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey), "%s %s", method, path)
		}
	}
}

// The exclusions above are exact segment matches, not prefixes or substrings.
// A customer slug that merely resembles a callback is still an MCP endpoint,
// and /platform/mcp/ registers no static siblings at all.
func TestMCPProtocolVersionTelemetryMatchesSlugsResemblingSiblingRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/mcp/idp_callback_service",
		"/mcp/my-remote_login_callback",
		"/platform/mcp/idp_callback",
		"/platform/mcp/remote_login_callback",
	} {
		got := recordSpanForRequest(t, http.MethodPost, path, mcpversions.Version20250618)
		require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)], "path %s", path)
	}
}

func TestMCPProtocolVersionTelemetryIgnoresNonMCPRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/rpc/toolsets.list", "/healthz", "/", "/mcp", "/x/mcp", "/x/other/slug"} {
		got := recordSpanForRequest(t, http.MethodPost, path, mcpversions.Version20250618)
		require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey), "path %s", path)
	}
}

func TestMCPProtocolVersionTelemetryOmitsAttributeWhenHeaderAbsent(t *testing.T) {
	t.Parallel()

	got := recordSpanForRequest(t, http.MethodPost, "/mcp/my-server", "")
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}

func TestMCPProtocolVersionTelemetryKeepsUnrecognizedButWellFormedValues(t *testing.T) {
	t.Parallel()

	// The span carries the raw value: an unrecognized revision is exactly what
	// an operator needs to see. Bucketing to "other" applies to metric
	// dimensions only.
	got := recordSpanForRequest(t, http.MethodPost, "/mcp/my-server", "1999-12-31")
	require.Equal(t, "1999-12-31", got[string(attr.McpNegotiatedProtocolVersionKey)])
}

func TestMCPProtocolVersionTelemetryDropsHostileHeaderValues(t *testing.T) {
	t.Parallel()

	got := recordSpanForRequest(t, http.MethodPost, "/mcp/my-server", "2025-06-18\x00injected")
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}

func TestMCPProtocolVersionTelemetryBoundsOversizedHeaderValues(t *testing.T) {
	t.Parallel()

	got := recordSpanForRequest(t, http.MethodPost, "/mcp/my-server", strings.Repeat("a", 4096))
	require.Len(t, got[string(attr.McpNegotiatedProtocolVersionKey)], 32)
}

func TestMCPProtocolVersionTelemetryPassesRequestThrough(t *testing.T) {
	t.Parallel()

	called := false
	var seenHeader string
	handler := middleware.MCPProtocolVersionTelemetry(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seenHeader = r.Header.Get("MCP-Protocol-Version")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-server", nil)
	req.Header.Set("MCP-Protocol-Version", mcpversions.Version20250618)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rec.Code)
	// The proxy forwards this header upstream verbatim, so the middleware must
	// not consume or rewrite it.
	require.Equal(t, mcpversions.Version20250618, seenHeader)
}

func TestMCPProtocolVersionTelemetryToleratesMissingSpan(t *testing.T) {
	t.Parallel()

	// Requests short-circuited above otelhttp reach handlers with no recording
	// span in context; the middleware must not panic.
	handler := middleware.MCPProtocolVersionTelemetry(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp/my-server", nil)
	req.Header.Set("MCP-Protocol-Version", mcpversions.Version20250618)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}
