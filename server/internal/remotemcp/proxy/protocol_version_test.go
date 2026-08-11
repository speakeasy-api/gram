package proxy_test

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
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// newRecordingProxyForTest builds the standard test proxy with a recording
// tracer swapped in, so the attributes it stamps on its own spans can be
// asserted. Deriving from newProxyForTest rather than rebuilding the struct
// keeps the two from drifting as fields are added to proxy.Proxy.
func newRecordingProxyForTest(t *testing.T, upstreamURL string) (*proxy.Proxy, *tracetest.SpanRecorder) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	p := newProxyForTest(t, upstreamURL)
	p.Tracer = provider.Tracer("test")

	return p, recorder
}

// postSpanAttributes returns the attributes on the span the proxy opens for the
// inbound POST. Selected by name rather than position: spans end
// children-before-parent, so any span added to the forward or relay path would
// otherwise take the first slot and be silently asserted against instead.
func postSpanAttributes(t *testing.T, recorder *tracetest.SpanRecorder) map[string]string {
	t.Helper()

	got := map[string]string{}
	found := false
	for _, span := range recorder.Ended() {
		if span.Name() != "remotemcp.proxy.Post" {
			continue
		}
		require.False(t, found, "expected exactly one remotemcp.proxy.Post span")
		found = true
		for _, kv := range span.Attributes() {
			got[string(kv.Key)] = kv.Value.AsString()
		}
	}
	require.True(t, found, "no remotemcp.proxy.Post span was recorded")

	return got
}

// TestProxy_Post_RecordsUpstreamDowngradeOnProxySpan is the case the whole
// initialize-response path exists for: the client asks for a newer revision and
// the upstream answers with an older one. Both values land on the proxy's own
// span so the downgrade is attributable to the upstream leg rather than reading
// as though Gram pinned the version.
func TestProxy_Post_RecordsUpstreamDowngradeOnProxySpan(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpRequestedProtocolVersionKey)])
	require.Equal(t, mcpversions.Version20250326, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

// TestProxy_Post_RecordsUpstreamUpgrade covers the direction that is easy to
// forget: go-sdk answers with its own latest revision when it does not
// recognize the client's, so upstreams upgrade as well as downgrade.
func TestProxy_Post_RecordsUpstreamUpgrade(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25"}}`))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20241105, got[string(attr.McpRequestedProtocolVersionKey)])
	require.Equal(t, mcpversions.Version20251125, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

// TestProxy_Post_RecordsNegotiatedVersionFromSSEInitialize pins that an
// upstream answering initialize over SSE is not a blind spot. Both response
// shapes are legal for a JSON-RPC request under Streamable HTTP.
func TestProxy_Post_RecordsNegotiatedVersionFromSSEInitialize(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20250326, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

// TestProxy_Post_OmitsNegotiatedVersionOnUpstreamError keeps a failed handshake
// from being recorded as a successful negotiation.
func TestProxy_Post_OmitsNegotiatedVersionOnUpstreamError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"unsupported protocol version"}}`))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20260728, got[string(attr.McpRequestedProtocolVersionKey)], "the client's ask is still recorded")
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey), "an error response negotiated nothing")
}

// TestProxy_Post_RecordsHeaderOnNonInitializeRequest covers the steady state:
// after the handshake every request carries the negotiated version in a header,
// which is what makes per-request attribution possible.
func TestProxy_Post_RecordsHeaderOnNonInitializeRequest(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcpversions.Version20250618)

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

// TestProxy_Post_ForwardsProtocolVersionHeaderUpstream pins that reading the
// header for telemetry does not stop the upstream from seeing it. The upstream
// negotiated this version with the client and depends on it.
func TestProxy_Post_ForwardsProtocolVersionHeaderUpstream(t *testing.T) {
	t.Parallel()

	var gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("MCP-Protocol-Version")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	p, _ := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcpversions.Version20250618)

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	require.Equal(t, mcpversions.Version20250618, gotHeader)
}

// TestProxy_Post_BoundsOversizedProtocolVersionHeader pins that a
// client-supplied header cannot write an unbounded string into telemetry.
// Control bytes need no coverage here: Go's HTTP transport rejects them before
// the request is forwarded, and middleware.MCPProtocolVersionTelemetry covers
// the sanitizer directly.
func TestProxy_Post_BoundsOversizedProtocolVersionHeader(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", strings.Repeat("a", 4096))

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Len(t, got[string(attr.McpNegotiatedProtocolVersionKey)], 32)
}

// TestProxy_Post_RecordsRequestedVersionWhenInterceptorRejects pins that a
// handshake refused by the generic interceptor chain is still attributed to a
// protocol revision. A rejected session is one of the more interesting things
// to be able to slice by version.
func TestProxy_Post_RecordsRequestedVersionWhenInterceptorRejects(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be reached when the request is rejected")
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{
		&mockUserRequestInterceptor{
			name:    "rejector",
			called:  nil,
			order:   nil,
			err:     &proxy.RejectError{Code: proxy.RejectCodeInvalidRequest, Message: "nope", Data: nil},
			mutator: nil,
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpRequestedProtocolVersionKey)])
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey), "nothing was negotiated; the request never reached upstream")
}

// TestProxy_Post_IgnoresNegotiatedVersionFromMismatchedResponseID keeps an
// upstream that answers with an id it was never asked about from being
// attributed as this session's negotiation.
func TestProxy_Post_IgnoresNegotiatedVersionFromMismatchedResponseID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":99,"result":{"protocolVersion":"2024-11-05"}}`))
	}))
	t.Cleanup(upstream.Close)

	p, recorder := newRecordingProxyForTest(t, upstream.URL)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/id", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	require.NoError(t, p.Post(httptest.NewRecorder(), req))

	got := postSpanAttributes(t, recorder)
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpRequestedProtocolVersionKey)])
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}
