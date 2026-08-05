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
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
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
	} {
		got := recordSpanForRequest(t, http.MethodPost, path, mcpversions.Version20250618)
		require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey), "path %s", path)
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
