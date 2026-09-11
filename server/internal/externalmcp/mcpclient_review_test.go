package externalmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestReviewDiscoveryJSONDiagnosticRedactsNumericValue(t *testing.T) {
	t.Parallel()
	const sentinel = "9876543210123456789e1000"
	listing := metadataListing(`{"type":"object","properties":{"private-field-sentinel":{"const":` + sentinel + `}}}`)
	// Prove that this valid JSON reaches SDK response decoding and produces
	// a typed decoder error whose original message contains the remote value.
	raw, err := json.Marshal(listing)
	require.NoError(t, err)
	var decoded mcp.ListToolsResult
	err = json.Unmarshal(raw, &decoded)
	var diagnostic *json.UnmarshalTypeError
	require.ErrorAs(t, err, &diagnostic)
	require.ErrorContains(t, err, sentinel)

	s := &parameterServer{schema: annotatedSchema, reject: func(int) (int, int) {
		return http.StatusBadRequest, mcp.CodeHeaderMismatch
	}}
	var lists atomic.Int64
	c := newMetadataClient(t, s, func(parameterRequest) any {
		lists.Add(1)
		return listing
	})()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{"owner":"example-owner"}`), nil)
	require.Error(t, err)
	require.Nil(t, result)
	require.NoError(t, ctx.Err())
	require.ErrorContains(t, err, "list tools from external mcp server")
	// The SDK may report offset zero; preserve the decoder metadata without
	// substituting the value-bearing original diagnostic.
	require.Regexp(t, `JSON type mismatch at byte offset [0-9]+ \(destination float64\)`, err.Error())
	require.NotContains(t, err.Error(), sentinel)
	require.NotContains(t, err.Error(), "private-field-sentinel")
	require.EqualValues(t, 1, lists.Load())
	require.Len(t, s.matching("tools/call"), 1, "failed discovery must not replay")
}

func TestReviewDiscoveryMismatchDoesNotExhaustReplay(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	logger := testenv.NewLogger(t)
	s := &parameterServer{schema: annotatedSchema, reject: func(int) (int, int) {
		return http.StatusBadRequest, mcp.CodeHeaderMismatch
	}}
	var lists atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var req parameterRequest
		if json.Unmarshal(body, &req) == nil && req.Method == "tools/list" {
			lists.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{
				"code": mcp.CodeHeaderMismatch, "message": "private-message-sentinel", "data": "private-data-sentinel",
			}})
			return
		}
		s.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := NewClient(ctx, logger, policy, server.URL, types.TransportTypeStreamableHTTP, &ClientOptions{Metrics: NewMetrics(provider, logger)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	result, err := c.CallTool(ctx, "lookup", json.RawMessage(`{"owner":"example-owner"}`), nil)
	require.Error(t, err)
	require.Nil(t, result)
	require.NoError(t, ctx.Err())
	var rpcErr *jsonrpc.Error
	require.ErrorAs(t, err, &rpcErr)
	require.EqualValues(t, mcp.CodeHeaderMismatch, rpcErr.Code)
	require.Nil(t, rpcErr.Data)
	require.NotContains(t, err.Error(), "private-message-sentinel")
	require.NotContains(t, err.Error(), "private-data-sentinel")
	require.EqualValues(t, 1, lists.Load())
	require.Len(t, s.matching("tools/call"), 1)
	require.Len(t, s.matching("server/discover"), 2, "recovery uses a fresh session")
	assertParameterRecoveryCounters(t, reader, map[string]int64{
		"externalmcp.header.mismatches":           1,
		"externalmcp.header.recovery.attempts":    1,
		"externalmcp.header.recovery.successes":   0,
		"externalmcp.header.recovery.failures":    1,
		"externalmcp.header.recovery.exhaustions": 0,
	})
}
