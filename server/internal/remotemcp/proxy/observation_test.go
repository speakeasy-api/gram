package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

func TestTunnelObservationRecordsOneSSETerminalOutcome(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[],\"isError\":true}}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[]}}\n\n"))
	}))
	defer upstream.Close()
	p := newProxyForTest(t, upstream.URL)
	var outcomes []string
	p.RequestObserver = func(method, client, outcome string, _ time.Duration) {
		require.Equal(t, "tools/call", method)
		require.Equal(t, "claude", client)
		outcomes = append(outcomes, outcome)
	}
	req := httptest.NewRequest(http.MethodPost, "http://gram/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"PRIVATE_TOOL","arguments":{"secret":"PRIVATE_ARGUMENT"}}}`)).WithContext(t.Context())
	req.Header.Set("User-Agent", "Claude/1.0 PRIVATE_IDENTITY")
	require.NoError(t, p.Post(httptest.NewRecorder(), req))
	require.Equal(t, []string{"attempt", "error"}, outcomes)
}
func TestTunnelObservationHTTPFailureIsNotMCPSuccess(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer upstream.Close()
	p := newProxyForTest(t, upstream.URL)
	var outcomes []string
	p.RequestObserver = func(_, _, outcome string, _ time.Duration) { outcomes = append(outcomes, outcome) }
	req := httptest.NewRequest(http.MethodPost, "http://gram/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)).WithContext(t.Context())
	require.NoError(t, p.Post(httptest.NewRecorder(), req))
	require.Equal(t, []string{"attempt", "error"}, outcomes)
}

func TestTunnelObservationRetryIsOneLogicalRequest(t *testing.T) {
	t.Parallel()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer bad.Close()
	p := newProxyForTest(t, bad.URL)
	p.UpstreamResponseRetryer = func(context.Context, *http.Response) (*proxy.UpstreamResponseRetry, error) {
		return &proxy.UpstreamResponseRetry{RemoteURL: good.URL, Headers: nil}, nil
	}
	var outcomes []string
	p.RequestObserver = func(_, _, outcome string, _ time.Duration) { outcomes = append(outcomes, outcome) }
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, p.Post(httptest.NewRecorder(), req))
	require.Equal(t, []string{"attempt", "success"}, outcomes)
}
func TestTunnelObservationPolicyRejectionIsTerminal(t *testing.T) {
	t.Parallel()
	p := newProxyForTest(t, "http://127.0.0.1:1")
	p.UserRequestInterceptors = []proxy.UserRequestInterceptor{&mockUserRequestInterceptor{name: "policy", err: &proxy.RejectError{Code: proxy.RejectCodeInvalidParams, Message: "PRIVATE_ERROR", Data: nil}}}
	var outcomes []string
	p.RequestObserver = func(_, _, outcome string, _ time.Duration) { outcomes = append(outcomes, outcome) }
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, p.Post(httptest.NewRecorder(), req))
	require.Equal(t, []string{"attempt", "error"}, outcomes)
}

func TestTunnelObservationTerminalErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"rpc_error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"PRIVATE_ERROR"}}`, 200},
		{"malformed_result", `{"jsonrpc":"2.0","id":1,"result":"not an object"}`, 200},
		{"redirect", `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`, 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			p := newProxyForTest(t, upstream.URL)
			var outcomes []string
			p.RequestObserver = func(_, _, outcome string, _ time.Duration) { outcomes = append(outcomes, outcome) }
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"fixture","arguments":{}}}`))
			_ = p.Post(httptest.NewRecorder(), req)
			require.Equal(t, []string{"attempt", "error"}, outcomes)
		})
	}
}

func TestTunnelObservationIdleStreamIsIncomplete(t *testing.T) {
	t.Parallel()
	upstream := newStallingSSEUpstream(t)
	p := newProxyForTest(t, upstream.URL)
	p.StreamingTimeout = 50 * time.Millisecond
	var outcomes []string
	p.RequestObserver = func(_, _, outcome string, _ time.Duration) { outcomes = append(outcomes, outcome) }
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.Error(t, p.Post(httptest.NewRecorder(), req))
	require.Equal(t, []string{"attempt", "incomplete"}, outcomes)
}
