package remotemcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newRequestCounterInterceptorForTest builds an interceptor backed by a
// testenv noop OTel meter for the pass-through tests, which verify the
// interceptor observes any message shape without ever rejecting. The
// recording contract itself is pinned with a manual reader in
// TestRequestOTELCounterInterceptor_RecordsCensusDatapoint.
func newRequestCounterInterceptorForTest(t *testing.T) *remotemcp.RequestOTELCounterInterceptor {
	t.Helper()
	return remotemcp.NewRequestOTELCounterInterceptor(
		mcpmetrics.NewRequestCounter(testenv.NewMeterProvider(t).Meter("test"), testenv.NewLogger(t)),
	)
}

func newRequestCounterUserRequest(t *testing.T, headerVersion string, messages ...jsonrpc.Message) *proxy.UserRequest {
	t.Helper()

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", http.NoBody)
	require.NoError(t, err)
	if headerVersion != "" {
		httpReq.Header.Set(mcpversions.HTTPHeader, headerVersion)
	}

	return &proxy.UserRequest{
		UserHTTPRequest: httpReq,
		JSONRPCMessages: messages,
	}
}

func TestRequestOTELCounterInterceptor_Name(t *testing.T) {
	t.Parallel()

	require.Equal(t, "request-otel-counter", newRequestCounterInterceptorForTest(t).Name())
}

func TestRequestOTELCounterInterceptor_NilRequestPassesThrough(t *testing.T) {
	t.Parallel()

	require.NoError(t, newRequestCounterInterceptorForTest(t).InterceptUserRequest(t.Context(), nil))
}

func TestRequestOTELCounterInterceptor_RecordsHeaderVersionRequest(t *testing.T) {
	t.Parallel()

	req := newRequestCounterUserRequest(t, mcpversions.Version20260728, &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/list",
		Params: json.RawMessage(`{}`),
		Extra:  nil,
	})

	require.NoError(t, newRequestCounterInterceptorForTest(t).InterceptUserRequest(t.Context(), req))
}

// TestRequestOTELCounterInterceptor_BodyMetaFallbackPassesThrough exercises the
// header-absent path where the declared version is read from the request's
// `_meta` instead.
func TestRequestOTELCounterInterceptor_BodyMetaFallbackPassesThrough(t *testing.T) {
	t.Parallel()

	req := newRequestCounterUserRequest(t, "", &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/call",
		Params: json.RawMessage(`{"name":"get_weather","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`),
		Extra:  nil,
	})

	require.NoError(t, newRequestCounterInterceptorForTest(t).InterceptUserRequest(t.Context(), req))
}

// TestRequestOTELCounterInterceptor_Pre20250618ClientWithoutVersionPassesThrough
// covers clients on 2024-11-05 and 2025-03-26, the revisions that predate the
// MCP-Protocol-Version header: no header and no per-request `_meta`, which
// must record (clamping to "none") without rejecting.
func TestRequestOTELCounterInterceptor_Pre20250618ClientWithoutVersionPassesThrough(t *testing.T) {
	t.Parallel()

	req := newRequestCounterUserRequest(t, "", &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/list",
		Params: json.RawMessage(`{}`),
		Extra:  nil,
	})

	require.NoError(t, newRequestCounterInterceptorForTest(t).InterceptUserRequest(t.Context(), req))
}

// TestRequestOTELCounterInterceptor_Version20250326InitializePassesThrough
// covers a 2025-03-26 initialize relayed through the proxy: the top-level
// requested version in the body is deliberately not a census input, so this
// exercises the header-less, `_meta`-less initialize shape end to end.
func TestRequestOTELCounterInterceptor_Version20250326InitializePassesThrough(t *testing.T) {
	t.Parallel()

	req := newRequestCounterUserRequest(t, "", &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":"2025-03-26","clientInfo":{"name":"handshake-client","version":"1.0.0"},"capabilities":{}}`),
		Extra:  nil,
	})

	require.NoError(t, newRequestCounterInterceptorForTest(t).InterceptUserRequest(t.Context(), req))
}

func TestRequestOTELCounterInterceptor_NonRequestMessagesAreSkipped(t *testing.T) {
	t.Parallel()

	// A response-shaped message must be ignored rather than counted or
	// rejected; a missing HTTP request must not panic.
	req := &proxy.UserRequest{
		UserHTTPRequest: nil,
		JSONRPCMessages: []jsonrpc.Message{&jsonrpc.Response{ID: jsonrpc.ID{}, Result: json.RawMessage(`{}`), Error: nil}},
	}

	require.NoError(t, newRequestCounterInterceptorForTest(t).InterceptUserRequest(t.Context(), req))
}

func TestRequestOTELCounterInterceptor_NilMetricsIsSafe(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewRequestOTELCounterInterceptor(nil)
	req := newRequestCounterUserRequest(t, mcpversions.Version20250618, &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "ping",
		Params: nil,
		Extra:  nil,
	})

	require.NoError(t, interceptor.InterceptUserRequest(t.Context(), req))
}

// TestRequestOTELCounterInterceptor_RecordsCensusDatapoint pins the
// interceptor's core contract with a real reader: a parseable request must
// produce an mcp.request datapoint carrying the clamped version, the clamped
// method, and the hosting surface. The pass-through tests above use a noop
// meter and cannot detect the recording being removed.
func TestRequestOTELCounterInterceptor_RecordsCensusDatapoint(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	interceptor := remotemcp.NewRequestOTELCounterInterceptor(mcpmetrics.NewRequestCounter(meter, testenv.NewLogger(t)))

	req := newRequestCounterUserRequest(t, mcpversions.Version20260728, &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "rpc.discover",
		Params: json.RawMessage(`{}`),
		Extra:  nil,
	})
	require.NoError(t, interceptor.InterceptUserRequest(t.Context(), req))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)

	var census metricdata.Metrics
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name == mcpmetrics.InstrumentMCPRequest {
			census = m
			break
		}
	}
	require.NotEmpty(t, census.Name, "mcp.request metric not found")

	metricdatatest.AssertHasAttributes(t, census,
		attr.MCPNegotiatedProtocolVersion(mcpversions.Version20260728),
		attr.McpMethod(mcprequests.MethodOther),
		attr.McpSurface(string(mcpmetrics.SurfaceHosting)),
	)
}
