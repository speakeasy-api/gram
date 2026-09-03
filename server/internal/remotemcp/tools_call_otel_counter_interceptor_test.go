package remotemcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newCounterInterceptorForTest(t *testing.T) (*ToolsCallOTELCounterInterceptor, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	logger := testenv.NewLogger(t)
	metrics := NewProxyMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), logger)
	return NewToolsCallOTELCounterInterceptor(metrics, proxy.ServerIdentity{
		RemoteMCPServerID: "srv-test",
		McpServerID:       "mcp-test",
	}, logger), reader
}

func newToolsCallUserRequest(params json.RawMessage) *proxy.UserRequest {
	return &proxy.UserRequest{JSONRPCMessages: []jsonrpc.Message{&jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/call",
		Params: params,
	}}}
}

func collectToolCallCounter(t *testing.T, reader *sdkmetric.ManualReader) (int64, string) {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	var calls int64
	var toolName string
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != instrumentMCPToolCall {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				calls += point.Value
				value, ok := point.Attributes.Value(attr.ToolName("").Key)
				require.True(t, ok)
				toolName = value.AsString()
			}
		}
	}
	return calls, toolName
}

func authenticatedCounterContext(t *testing.T) context.Context {
	t.Helper()

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-counter",
		AccountType:          string(billing.TierPro),
	})
	return contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{
		Host:   "x.example.com",
		ReqURL: "/x/mcp/abc",
	})
}

func TestToolsCallOTELCounterInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor, _ := newCounterInterceptorForTest(t)
	require.Equal(t, "tools-call-otel-counter", interceptor.Name())
}

func TestToolsCallOTELCounterInterceptor_NoAuthContextPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor, _ := newCounterInterceptorForTest(t)
	require.NoError(t, interceptor.InterceptUserRequest(t.Context(), newToolsCallUserRequest(json.RawMessage(`{"name":"any_tool"}`))))
}

func TestToolsCallOTELCounterInterceptor_RecordsTypedRequest(t *testing.T) {
	t.Parallel()

	interceptor, reader := newCounterInterceptorForTest(t)
	require.NoError(t, interceptor.InterceptUserRequest(authenticatedCounterContext(t), newToolsCallUserRequest(json.RawMessage(`{"name":"search_tickets"}`))))

	calls, toolName := collectToolCallCounter(t, reader)
	require.Equal(t, int64(1), calls)
	require.Equal(t, "search_tickets", toolName)
}

func TestToolsCallOTELCounterInterceptor_RecordsMalformedParamsWithoutRejecting(t *testing.T) {
	t.Parallel()

	interceptor, reader := newCounterInterceptorForTest(t)
	require.NoError(t, interceptor.InterceptUserRequest(authenticatedCounterContext(t), newToolsCallUserRequest(json.RawMessage(`"malformed params"`))))

	calls, toolName := collectToolCallCounter(t, reader)
	require.Equal(t, int64(1), calls)
	require.Empty(t, toolName)
}

func TestToolsCallOTELCounterInterceptor_NeverRejects(t *testing.T) {
	t.Parallel()

	interceptor, _ := newCounterInterceptorForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{})

	require.NoError(t, interceptor.InterceptUserRequest(ctx, newToolsCallUserRequest(json.RawMessage(`{}`))))
}
