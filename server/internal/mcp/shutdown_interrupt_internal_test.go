package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shutdowndrain"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// errInterruptedFunctionCall is what the gateway returns for a function call
// the shutdown drain abandoned.
var errInterruptedFunctionCall = fmt.Errorf("%w: execute request: context canceled", shutdowndrain.ErrInterrupted)

func shutdownInterruptedPoints(t *testing.T, reader sdkmetric.Reader) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "mcp.tool_call.shutdown_interrupted" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "mcp.tool_call.shutdown_interrupted must be an int64 counter")
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}

	return points
}

func newShutdownTestMetrics(t *testing.T) (*mcpmetrics.Metrics, sdkmetric.Reader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	return mcpmetrics.NewMetrics(meter, testenv.NewLogger(t)), reader
}

func TestInterruptedAtShutdown_ToolsCallAnswer(t *testing.T) {
	t.Parallel()

	metrics, reader := newShutdownTestMetrics(t)
	logAttrs := tm.HTTPLogAttributes{}

	interruption := interruptedAtShutdown(t.Context(), testenv.NewLogger(t), metrics, logAttrs, mcpversions.MethodToolsCall, "function", errInterruptedFunctionCall)

	// The hosted answer matches what the relayed proxy writes for an
	// interrupted tools/call, apart from the message naming the tool.
	response := oops.NewMCPErrorFromCause(mcpjsonrpc.NumberID(7), "", interruption.Response)
	body, err := response.MarshalJSONFor("")
	require.NoError(t, err)
	require.JSONEq(t, `{
		"jsonrpc": "2.0",
		"id": 7,
		"error": {
			"code": -32603,
			"message": "Speakeasy is restarting and stopped waiting for the tool's response. The tool call may still have completed.",
			"data": {"reason": "server_shutdown", "retryable": false}
		}
	}`, string(body))

	require.Equal(t, oops.CodeUnavailable, interruption.Failure.Code)
	require.Equal(t, http.StatusServiceUnavailable, interruption.Failure.HTTPStatus(t.Context()))
	require.ErrorIs(t, interruption.Failure, shutdowndrain.ErrInterrupted)
	require.Equal(t, "server_shutdown", logAttrs[attr.ToolCallInterruptReasonKey])

	points := shutdownInterruptedPoints(t, reader)
	require.Equal(t, map[attribute.Set]int64{
		attribute.NewSet(attr.McpMethod(mcpversions.MethodToolsCall), attr.ToolCallKind("function")): 1,
	}, points)
}

func TestInterruptedAtShutdown_ResourcesReadIsRetryable(t *testing.T) {
	t.Parallel()

	metrics, reader := newShutdownTestMetrics(t)

	interruption := interruptedAtShutdown(t.Context(), testenv.NewLogger(t), metrics, tm.HTTPLogAttributes{}, mcpversions.MethodResourcesRead, "function", errInterruptedFunctionCall)

	require.Equal(t, oops.MCPCodeInternalError, interruption.Response.Code)
	require.Equal(t, resourceReadShutdownMessage, interruption.Response.Message)
	require.NotNil(t, interruption.Response.Data)
	require.Equal(t, oops.MCPErrorDataReasonServerShutdown, interruption.Response.Data.Reason)
	require.NotNil(t, interruption.Response.Data.Retryable)
	require.True(t, *interruption.Response.Data.Retryable, "a read has no side effects, so it can be retried")

	points := shutdownInterruptedPoints(t, reader)
	require.Equal(t, map[attribute.Set]int64{
		attribute.NewSet(attr.McpMethod(mcpversions.MethodResourcesRead), attr.ToolCallKind("function")): 1,
	}, points)
}

func TestInterruptedAtShutdown_AnsweredWithOKOnEveryRevision(t *testing.T) {
	t.Parallel()

	// Clients on the handshake revisions read a non-2xx as a transport
	// failure, and 2026-07-28 mandates no status for -32603, so the error
	// must travel in a 200 either way.
	for _, revision := range []string{"", mcpversions.Version20251125, mcpversions.Version20260728} {
		t.Run(fmt.Sprintf("revision %q", revision), func(t *testing.T) {
			t.Parallel()

			interruption := interruptedAtShutdown(t.Context(), testenv.NewLogger(t), nil, nil, mcpversions.MethodToolsCall, "http", errInterruptedFunctionCall)
			recorder := httptest.NewRecorder()

			err := writeMCPError(t.Context(), testenv.NewLogger(t), recorder, mcpjsonrpc.NumberID(1), revision, interruption.Response)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}

func TestInterruptedAtShutdown_PassesThroughMetaMemberDegradation(t *testing.T) {
	t.Parallel()

	// Meta server member dispatch turns shareable errors into a member tool
	// error; the shutdown answer must stay a JSON-RPC error instead.
	interruption := interruptedAtShutdown(t.Context(), testenv.NewLogger(t), nil, nil, mcpversions.MethodToolsCall, "function", errInterruptedFunctionCall)

	var shareable *oops.ShareableError
	require.NotErrorAs(t, interruption.Response, &shareable)
}
