package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestToolProxy_Do_ExternalMCP_RecoveryMetersOnce(t *testing.T) {
	t.Parallel()
	backend := newExternalMCPTestServer(t, false)
	var calls, lists atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Method == "tools/list" {
			lists.Add(1)
		}
		if req.Method == "tools/call" && calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32020, "message": "header mismatch"}})
			return
		}
		backend.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	plan := externalMCPPlan(upstream.URL)
	plan.InputSchema = json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Old"}}}`)
	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(newExternalMCPToolDescriptor(), plan), `{"owner":"sample"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 1, lists.Load())
	// The helper asserts exactly one metric point whose counter value is one.
	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(toolCallOutcomeSuccess), attributeValue(t, set, attr.OutcomeKey))
}
