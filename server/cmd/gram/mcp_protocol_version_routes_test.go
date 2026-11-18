package gram

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
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
)

// The tests in this file check middleware.MCPProtocolVersionTelemetry against
// the routes the server mounts. They live with the server wiring rather than
// in the middleware package because the packages that own those routes import
// the middleware.

// recordMCPProtocolVersionSpan runs the middleware inside a recorded span,
// mimicking otelhttp having already opened the server span, and returns the
// attributes left on that span.
func recordMCPProtocolVersionSpan(t *testing.T, method, path, headerValue string) map[string]string {
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

	patterns := []string{mcp.PublicServerRoute, mcp.PublicServerRoute + "/", mcp.PlatformToolsetRoute}
	methods := []string{http.MethodPost, http.MethodGet, http.MethodDelete}

	for _, pattern := range patterns {
		path := routePathForSlug(t, pattern, "my-server")
		for _, method := range methods {
			got := recordMCPProtocolVersionSpan(t, method, path, mcpversions.Version20250618)
			require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)],
				"route %q resolved to %q, which the middleware does not match for %s", pattern, path, method)
		}
	}

	// Speakeasy's own platform MCP server carries no slug, and it is
	// registered for POST only (platform_mcp.go), hence no verb cross-product.
	got := recordMCPProtocolVersionSpan(t, http.MethodPost, platformmcp.Path, mcpversions.Version20250618)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)],
		"%s is an MCP JSON-RPC endpoint and must be matched", platformmcp.Path)

	// The Approvals MCP server also carries no slug. It is registered for POST
	// only.
	got = recordMCPProtocolVersionSpan(t, http.MethodPost, approval.MCPPath, mcpversions.Version20250618)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)],
		"%s is an MCP JSON-RPC endpoint and must be matched", approval.MCPPath)

	// The per-agent gateway is addressed by agent id rather than slug and is
	// registered for POST only, hence no verb cross-product.
	agentPath := routePathForSlug(t, mcp.AgentGatewayRoute, "0e3b6b1a-0000-4000-8000-000000000001")
	got = recordMCPProtocolVersionSpan(t, http.MethodPost, agentPath, mcpversions.Version20250618)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)],
		"%s is an MCP JSON-RPC endpoint and must be matched", mcp.AgentGatewayRoute)
}

// TestMCPProtocolVersionTelemetryIgnoresApprovalsSubRoutes checks that only
// the Approvals MCP path itself is matched, not paths beneath it.
func TestMCPProtocolVersionTelemetryIgnoresApprovalsSubRoutes(t *testing.T) {
	t.Parallel()

	got := recordMCPProtocolVersionSpan(t, http.MethodPost, approval.MCPPath+"/extra", mcpversions.Version20250618)
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}
