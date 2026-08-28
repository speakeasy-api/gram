package remotemcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type countingToolsCallInterceptor struct {
	calls atomic.Int32
}

func (i *countingToolsCallInterceptor) Name() string { return "protected-work-spy" }

func (i *countingToolsCallInterceptor) InterceptToolsCallRequest(context.Context, *proxy.ToolsCallRequest) error {
	i.calls.Add(1)
	return nil
}

func TestToolsCallKillswitchStopsProtectedAndUpstreamWork(t *testing.T) {
	t.Parallel()

	match, err := killswitches.NewMatchedDenialDisposition("Calls paused by an administrator.")
	require.NoError(t, err)
	cases := []struct {
		name        string
		disposition killswitches.TransportDisposition
		err         error
		wantError   map[string]any
	}{
		{
			name:        "matched prescription",
			disposition: match,
			wantError: map[string]any{
				"code":    float64(proxy.RejectCodeForbidden),
				"message": "Calls paused by an administrator.",
				"data":    map[string]any{"code": proxy.KillswitchRejectionCode},
			},
		},
		{
			name:        "evaluator infrastructure failure",
			disposition: killswitches.NewInfrastructureRejectionDisposition(),
			err:         context.DeadlineExceeded,
			wantError: map[string]any{
				"code":    float64(proxy.RejectCodeInternalError),
				"message": "service temporarily unavailable",
				"data":    nil,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{}}`))
			}))
			t.Cleanup(upstream.Close)

			checkpoint := &fakeKillswitchCheckpoint{disposition: tc.disposition, err: tc.err}
			protectedWork := &countingToolsCallInterceptor{}
			p := newKillswitchTestProxy(t, upstream.URL,
				NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)),
				protectedWork,
			)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"protected","arguments":{}}}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			rr := httptest.NewRecorder()

			require.NoError(t, p.Post(rr, req))
			require.Equal(t, http.StatusOK, rr.Code)
			require.Zero(t, upstreamCalls.Load())
			require.Zero(t, protectedWork.calls.Load())
			require.Equal(t, 1, checkpoint.calls)

			var envelope map[string]any
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
			id, ok := envelope["id"].(float64)
			require.True(t, ok)
			require.InDelta(t, 7, id, 0)
			require.Equal(t, "2.0", envelope["jsonrpc"])
			require.Equal(t, tc.wantError, envelope["error"])
		})
	}
}

func TestToolsCallKillswitchRejectsMalformedParamsAfterCheckpoint(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	checkpoint := &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()}
	protectedWork := &countingToolsCallInterceptor{}
	p := newKillswitchTestProxy(t, upstream.URL,
		NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)),
		protectedWork,
	)
	bodies := []string{
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":[]}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":null}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call"}`,
	}
	for _, body := range bodies {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rr := httptest.NewRecorder()

		require.NoError(t, p.Post(rr, req))
		require.Equal(t, http.StatusOK, rr.Code)

		var envelope map[string]any
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
		require.Equal(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      float64(7),
			"error": map[string]any{
				"code":    float64(proxy.RejectCodeInvalidParams),
				"message": "malformed tools/call request",
				"data":    nil,
			},
		}, envelope)
	}

	require.Equal(t, len(bodies), checkpoint.calls)
	require.Zero(t, protectedWork.calls.Load())
	require.Zero(t, upstreamCalls.Load())
}

func TestToolsCallKillswitchEvaluatesAmbiguousStrictSessionCalls(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	checkpoint := &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()}
	protectedWork := &countingToolsCallInterceptor{}
	p := newKillswitchTestProxy(t, upstream.URL,
		NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)),
		protectedWork,
	)
	// A non-nil session selection enables strict validation in the proxy
	// factory. Model that configuration here while keeping the policy
	// checkpoint and protected-work spy explicit.
	p.StrictToolSelection = true

	bodies := []string{
		`{"jsonrpc":"2.0","id":7,"method":"ping","method":"tools/call","params":{"name":"protected","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","method":"ping","params":{"name":"protected","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"protected"},"params":[]}`,
	}
	for i, body := range bodies {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rr := httptest.NewRecorder()

		require.NoError(t, p.Post(rr, req))
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, i+1, checkpoint.calls, "each ambiguous tools/call must evaluate exactly once")

		var envelope map[string]any
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
		require.Equal(t, "2.0", envelope["jsonrpc"])
		require.InDelta(t, 7, envelope["id"], 0)
		rpcErr, ok := envelope["error"].(map[string]any)
		require.True(t, ok)
		require.InDelta(t, proxy.RejectCodeInvalidRequest, rpcErr["code"], 0)
		require.Contains(t, rpcErr["message"], "ambiguous JSON-RPC message")
		require.Nil(t, rpcErr["data"])
	}

	require.Zero(t, protectedWork.calls.Load())
	require.Zero(t, upstreamCalls.Load())
}

func TestToolsCallKillswitchDeniesExistingSessionOnNextCall(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "existing-session")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	checkpoint := &fakeKillswitchCheckpoint{disposition: killswitches.NewContinueDisposition()}
	p := newKillswitchTestProxy(t, upstream.URL, NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)))
	initialize := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	initialize.Header.Set("Content-Type", "application/json")
	initialize.Header.Set("Accept", "application/json, text/event-stream")
	initializeResponse := httptest.NewRecorder()
	require.NoError(t, p.Post(initializeResponse, initialize))
	require.Equal(t, "existing-session", initializeResponse.Header().Get("Mcp-Session-Id"))
	require.Equal(t, int32(1), upstreamCalls.Load())
	require.Zero(t, checkpoint.calls)

	match, err := killswitches.NewMatchedDenialDisposition("Session calls paused.")
	require.NoError(t, err)
	checkpoint.disposition = match
	call := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"protected","arguments":{}}}`))
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Accept", "application/json, text/event-stream")
	call.Header.Set("Mcp-Session-Id", "existing-session")
	callResponse := httptest.NewRecorder()
	require.NoError(t, p.Post(callResponse, call))
	require.Equal(t, int32(1), upstreamCalls.Load(), "post-activation call must not reach the established upstream session")
	require.Equal(t, 1, checkpoint.calls)
	require.Contains(t, callResponse.Body.String(), "Session calls paused.")
}

func TestToolsCallKillswitchExcludesOtherMethods(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	checkpoint := &fakeKillswitchCheckpoint{disposition: killswitches.NewInfrastructureRejectionDisposition(), err: context.DeadlineExceeded}
	p := newKillswitchTestProxy(t, upstream.URL, NewToolsCallKillswitchInterceptor(checkpoint, "organization-id", "server-id", testenv.NewLogger(t)))
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`,
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/server", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		require.NoError(t, p.Post(httptest.NewRecorder(), req))
	}
	require.Equal(t, int32(3), upstreamCalls.Load())
	require.Zero(t, checkpoint.calls)
}

func newKillswitchTestProxy(t *testing.T, upstreamURL string, interceptors ...proxy.ToolsCallRequestInterceptor) *proxy.Proxy {
	t.Helper()
	require.NotEmpty(t, interceptors)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	return &proxy.Proxy{
		GuardianPolicy:                  policy,
		Logger:                          testenv.NewLogger(t),
		Tracer:                          tracerProvider.Tracer("killswitch-test"),
		NonStreamingTimeout:             5 * time.Second,
		StreamingTimeout:                5 * time.Second,
		MaxBufferedBodyBytes:            proxy.DefaultMaxBufferedBodyBytes,
		RemoteURL:                       upstreamURL,
		ToolsCallPreForwardInterceptors: interceptors[:1],
		ToolsCallRequestInterceptors:    interceptors[1:],
	}
}
