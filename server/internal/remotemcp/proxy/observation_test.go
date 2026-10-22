package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	require.Equal(t, []string{"attempt", "rejected"}, outcomes, "a Speakeasy policy rejection never reached the MCP server, so it is not an error")
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

type fakeSessionClients struct {
	mu       sync.Mutex
	families map[string]string
	recorded []string
}

func (f *fakeSessionClients) RecordInitialize(_ context.Context, sessionID string, params *mcp.InitializeParams) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, sessionID+"="+params.ClientInfo.Name)
}

func (f *fakeSessionClients) ClientFamily(_ context.Context, sessionID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.families[sessionID]
}

func (f *fakeSessionClients) recordedSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.recorded)
}

func newSessionUpstream(t *testing.T, status int) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(proxy.McpSessionIDHeader, "upstream-session")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func TestTunnelObservationAttributesInitializeToReportedClient(t *testing.T) {
	t.Parallel()
	p := newProxyForTest(t, newSessionUpstream(t, http.StatusOK).URL)
	sessions := &fakeSessionClients{families: map[string]string{}}
	p.SessionClients = sessions
	var clients []string
	p.RequestObserver = func(_, client, _ string, _ time.Duration) { clients = append(clients, client) }
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cursor-vscode","version":"1.0"}}}`))
	req.Header.Set("User-Agent", "node")
	require.NoError(t, p.Post(httptest.NewRecorder(), req))
	require.Equal(t, []string{"cursor", "cursor"}, clients, "the initialize uses its own clientInfo, not the generic User-Agent")
	require.Equal(t, []string{"upstream-session=cursor-vscode"}, sessions.recordedSessions(), "the session is remembered under the id the client was answered with")
}

func TestTunnelObservationRemembersOnlySuccessfulInitialize(t *testing.T) {
	t.Parallel()
	p := newProxyForTest(t, newSessionUpstream(t, http.StatusInternalServerError).URL)
	sessions := &fakeSessionClients{families: map[string]string{}}
	p.SessionClients = sessions
	p.RequestObserver = func(string, string, string, time.Duration) {}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cursor-vscode","version":"1.0"}}}`))
	_ = p.Post(httptest.NewRecorder(), req)
	require.Empty(t, sessions.recordedSessions())
}

func TestTunnelObservationAttributesSessionRequestsToRecordedClient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, session, userAgent, want string
	}{
		{name: "recorded session", session: "known", userAgent: "node", want: "claude"},
		{name: "unknown session falls back to user agent", session: "unknown", userAgent: "Claude-User/1.0", want: "claude"},
		{name: "unknown session with generic user agent", session: "unknown", userAgent: "node", want: "other"},
		{name: "recorded unrecognized client defers to user agent", session: "unrecognized", userAgent: "codex-mcp-client/1.0", want: "codex"},
		{name: "no session", session: "", userAgent: "", want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
			}))
			defer upstream.Close()
			p := newProxyForTest(t, upstream.URL)
			p.SessionClients = &fakeSessionClients{families: map[string]string{"known": "claude", "unrecognized": "other"}}
			var clients []string
			p.RequestObserver = func(_, client, _ string, _ time.Duration) { clients = append(clients, client) }
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("User-Agent", tc.userAgent)
			if tc.session != "" {
				req.Header.Set(proxy.McpSessionIDHeader, tc.session)
			}
			require.NoError(t, p.Post(httptest.NewRecorder(), req))
			require.Equal(t, []string{tc.want, tc.want}, clients)
		})
	}
}
