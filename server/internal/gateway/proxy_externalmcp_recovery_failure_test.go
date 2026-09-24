package gateway

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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Recovery is internal to a single ToolProxy.Do invocation. Neither a failed
// refresh nor exhausting the replay budget may increment logical usage twice.
func TestToolProxy_Do_ExternalMCP_FailedRecoveryMetersOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		failRefresh bool
		wantCalls   int64
	}{
		{name: "refresh_failure", failRefresh: true, wantCalls: 1},
		{name: "replay_exhaustion", wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backend := newExternalMCPTestServer(t, false)
			var calls, lists atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var req struct {
					Method string          `json:"method"`
					ID     json.RawMessage `json:"id"`
				}
				_ = json.Unmarshal(body, &req)
				code := 0
				switch req.Method {
				case "tools/list":
					lists.Add(1)
					if tc.failRefresh {
						code = -32603
					}
				case "tools/call":
					calls.Add(1)
					code = mcp.CodeHeaderMismatch
				}
				if code != 0 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": "test protocol failure"}})
					return
				}
				backend.Config.Handler.ServeHTTP(w, r)
			}))
			t.Cleanup(upstream.Close)
			reader := sdkmetric.NewManualReader()
			proxy := newMetricToolProxy(t, reader)
			plan := externalMCPPlan(upstream.URL)
			plan.InputSchema = json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","x-mcp-header":"Old"}}}`)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := callToolProxy(t, ctx, proxy, NewExternalMCPToolCallPlan(newExternalMCPToolDescriptor(), plan), `{"owner":"sample"}`)
			require.Error(t, err)
			require.NoError(t, ctx.Err(), "recovery must stop without waiting for the deadline")
			require.Equal(t, tc.wantCalls, calls.Load())
			require.EqualValues(t, 1, lists.Load())
			// This helper requires one series with counter value one.
			set := onlyToolCallAttributes(t, reader)
			require.Equal(t, string(toolCallOutcomeNoResponse), attributeValue(t, set, attr.OutcomeKey))
		})
	}
}
