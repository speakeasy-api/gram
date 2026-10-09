package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A buffered tools/call failure must leave session cleanup to the SDK, which
// already observed the initialization response. Only hidden initial sessions
// need the adapter's fallback DELETE.
func TestMemberClientEstablishedSessionClosesOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	deletes := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deletes++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "server/discover":
			w.WriteHeader(http.StatusNotFound)
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "owned-by-sdk")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fixture","version":"1"}}}`, req.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			w.Header().Set("Mcp-Session-Id", "owned-by-sdk")
			// A body read failure occurs after the proxy observes the session header.
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte(`{"jsonrpc":`))
		}
	}))
	t.Cleanup(upstream.Close)
	service, build := memberClientFixture(t, upstream.URL)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second*5)
	defer cancel()
	session, _, err := service.connectMetaMember(ctx, testenv.NewLogger(t), build, time.Second)
	require.NoError(t, err)
	_, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "ping"})
	require.Error(t, err)
	require.NoError(t, session.Close())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, deletes)
}

func memberClientFixture(t *testing.T, endpoint string) (*Service, memberProxyBuilder) {
	t.Helper()
	tp := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tp, nil)
	require.NoError(t, err)
	return &Service{guardianPolicy: policy}, func(context.Context) (*proxy.Proxy, error) {
		return &proxy.Proxy{
			GuardianPolicy: policy, Logger: testenv.NewLogger(t), Tracer: tp.Tracer("test"),
			NonStreamingTimeout: 5 * time.Second, StreamingTimeout: 5 * time.Second,
			MaxBufferedBodyBytes: proxy.DefaultMaxBufferedBodyBytes, RemoteURL: endpoint,
		}, nil
	}
}

func TestMemberClientPreservesRichToolResult(t *testing.T) {
	t.Parallel()
	for _, contentType := range []string{"application/json", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			t.Parallel()
			// Exact bytes matter: JSONEq decodes numbers as float64 and cannot detect this regression.
			const result = `{"content":[{"type":"text","text":"pong","annotations":{"audience":["user"]}},{"type":"image","data":"aGVsbG8=","mimeType":"image/png"},{"type":"resource_link","uri":"https://example.com/report","name":"report"}],"structuredContent":{"items":[9007199254740993,0.12345678901234567890123456789,true]},"_meta":{"vendor":{"trace":"opaque","id":9007199254740993,"ratio":0.12345678901234567890123456789}},"isError":true,"vendorExtension":{"id":9007199254740993}}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch req.Method {
				case "server/discover":
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"supportedVersions":["2026-07-28"],"capabilities":{"tools":{}},"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"fixture","version":"1"}}}}`, req.ID)
				case "tools/call":
					w.Header().Set("Content-Type", contentType)
					if contentType == "text/event-stream" {
						_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":1,\"progress\":1}}\n\n")
						_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":\"unrelated\",\"result\":{\"content\":[]}}\n\n")
						_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", req.ID, result)
					} else {
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
					}
				default:
					http.Error(w, "unexpected method", http.StatusBadRequest)
				}
			}))
			t.Cleanup(upstream.Close)
			service, build := memberClientFixture(t, upstream.URL)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			session, _, err := service.connectMetaMember(ctx, testenv.NewLogger(t), build, time.Second)
			require.NoError(t, err)
			got, err := callMemberTool(ctx, session, &sdk.CallToolParams{Name: "ping"})
			require.NoError(t, err)
			require.NoError(t, session.Close())
			require.Equal(t, result, string(got)) //nolint:testifylint // JSONEq converts numbers to float64 and would hide precision loss.
		})
	}
}

func TestMemberClientFailedInitializationClosesOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		result string
	}{
		{name: "unsupported revision", status: http.StatusOK, result: `{"protocolVersion":"2099-01-01","capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}`},
		{name: "missing revision", status: http.StatusOK, result: `{"capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}`},
		{name: "unauthorized", status: http.StatusUnauthorized, result: `{}`},
		{name: "server error", status: http.StatusInternalServerError, result: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			deletes := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					mu.Lock()
					deletes++
					mu.Unlock()
					if r.Header.Get("Mcp-Session-Id") != "failed-init-session" {
						http.Error(w, "wrong session", http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusOK)
					return
				}
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if req.Method == "server/discover" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "failed-init-session")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, tc.result)
			}))
			t.Cleanup(upstream.Close)
			service, build := memberClientFixture(t, upstream.URL)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			session, _, err := service.connectMetaMember(ctx, testenv.NewLogger(t), build, time.Second)
			require.Error(t, err)
			require.Nil(t, session)
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, 1, deletes)
		})
	}
}

func TestMemberClientFailedInitializationCloseIsBounded(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	deletes := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deletes++
			mu.Unlock()
			<-r.Context().Done()
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Method == "server/discover" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "failed-init-session")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2099-01-01","capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}}`, req.ID)
	}))
	t.Cleanup(upstream.Close)
	service, build := memberClientFixture(t, upstream.URL)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	session, _, err := service.connectMetaMember(ctx, testenv.NewLogger(t), build, 100*time.Millisecond)
	require.Error(t, err)
	require.Nil(t, session)
	require.Less(t, time.Since(started), 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, deletes)
}

// Capturing the original result must never bypass a policy interceptor's rejection.
func TestMemberClientResultCaptureHonorsRejection(t *testing.T) {
	t.Parallel()
	server := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "must not be returned"}}}, nil
	})
	upstream := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil))
	t.Cleanup(upstream.Close)
	service, build := memberClientFixture(t, upstream.URL)
	guardedBuild := func(ctx context.Context) (*proxy.Proxy, error) {
		p, err := build(ctx)
		if err != nil {
			return nil, err
		}
		p.ToolsCallResponseInterceptors = append(p.ToolsCallResponseInterceptors, rejectMemberResult{})
		return p, nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, _, err := service.connectMetaMember(ctx, testenv.NewLogger(t), guardedBuild, time.Second)
	require.NoError(t, err)
	raw, err := callMemberTool(ctx, session, &sdk.CallToolParams{Name: "ping"})
	require.Error(t, err)
	require.Empty(t, raw)
	require.NoError(t, session.Close())
}

type rejectMemberResult struct{}

func (rejectMemberResult) Name() string { return "reject_member_result" }

func (rejectMemberResult) InterceptToolsCallResponse(context.Context, *proxy.ToolsCallResponse) error {
	return fmt.Errorf("response refused by policy")
}

// The SDK re-sends tools/call after an input_required answer on the same
// context; only the final answer, which CallTool accepted, may be relayed.
func TestMemberClientCapturesFinalMultiRoundTripResult(t *testing.T) {
	t.Parallel()
	const final = `{"content":[{"type":"text","text":"done"}],"structuredContent":{"id":9007199254740993}}`
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				InputResponses json.RawMessage `json:"inputResponses"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "server/discover":
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"supportedVersions":["2026-07-28"],"capabilities":{"tools":{}},"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"fixture","version":"1"}}}}`, req.ID)
		case "tools/call":
			mu.Lock()
			calls++
			mu.Unlock()
			if len(req.Params.InputResponses) == 0 {
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"input_required","inputRequests":{"roots":{"method":"roots/list","params":{}}},"requestState":"opaque"}}`, req.ID)
				return
			}
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, final)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	t.Cleanup(upstream.Close)
	service, build := memberClientFixture(t, upstream.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, _, err := service.connectMetaMember(ctx, testenv.NewLogger(t), build, time.Second)
	require.NoError(t, err)
	got, err := callMemberTool(ctx, session, &sdk.CallToolParams{Name: "ping"})
	require.NoError(t, err)
	require.NoError(t, session.Close())
	require.Equal(t, final, string(got)) //nolint:testifylint // JSONEq converts numbers to float64 and would hide precision loss.
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, calls)
}

func TestMemberToolResultIgnoresUncorrelatedRequests(t *testing.T) {
	t.Parallel()
	id, err := jsonrpc.MakeID(float64(1))
	require.NoError(t, err)
	response := &jsonrpc.Response{ID: id, Result: json.RawMessage(`{"content":[]}`)}
	for name, call := range map[string]*proxy.ToolsCallResponse{
		"no request":      {RemoteMessage: &proxy.RemoteMessage{Message: response}},
		"no user request": {RemoteMessage: &proxy.RemoteMessage{Message: response}, Request: &proxy.ToolsCallRequest{}},
		"no messages":     {RemoteMessage: &proxy.RemoteMessage{Message: response}, Request: &proxy.ToolsCallRequest{UserRequest: &proxy.UserRequest{}}},
		"no remote":       {Request: &proxy.ToolsCallRequest{UserRequest: &proxy.UserRequest{}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			capture := &memberToolResult{}
			require.NotPanics(t, func() {
				require.NoError(t, capture.InterceptToolsCallResponse(t.Context(), call))
			})
			require.Nil(t, capture.raw)
		})
	}
}

// Failures must name the exchange that failed, not an earlier leg: a legacy
// member answers server/discover with 404 before the SDK falls back.
func TestMemberClientFailureClassifiesUnansweredExchanges(t *testing.T) {
	t.Parallel()
	member := metaMember{slug: "fixture"}
	legacyThenDrop := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "server/discover" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}
	for name, tc := range map[string]struct {
		unroutable bool
		want       string
	}{
		"no route":      {unroutable: true, want: `server "fixture" is not reachable right now`},
		"dropped reply": {unroutable: false, want: `server "fixture" did not answer: upstream unreachable or timed out`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(http.HandlerFunc(legacyThenDrop))
			t.Cleanup(upstream.Close)
			service, build := memberClientFixture(t, upstream.URL)
			if tc.unroutable {
				build = func(context.Context) (*proxy.Proxy, error) { return nil, errors.New("tunnel has no live route") }
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, rt, err := service.connectMetaMember(ctx, testenv.NewLogger(t), build, time.Second)
			require.Error(t, err)
			got := memberClientFailure(ctx, testenv.NewLogger(t), rt, memberDial{build: build, anonymous: false}, member, err)
			require.EqualError(t, got, tc.want)
		})
	}
}

func TestMemberClientFailurePreservesClientCredentialRenewalRemedy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "outage", err: remotesessions.ErrClientCredentialUnavailable, want: "retry shortly"},
		{name: "misconfigured", err: remotesessions.ErrClientCredentialMisconfigured, want: "contact the MCP server administrator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(upstream.Close)
			service, build := memberClientFixture(t, upstream.URL)
			buildWithRenewal := func(ctx context.Context) (*proxy.Proxy, error) {
				p, err := build(ctx)
				if err != nil {
					return nil, err
				}
				renewal := &clientCredentialRenewal{}
				p.UpstreamResponseRetryer = func(context.Context, *http.Response) (*proxy.UpstreamResponseRetry, error) {
					renewal.err = tc.err
					return nil, nil
				}
				renewal.preserveFailure(p)
				return p, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, rt, err := service.connectMetaMember(ctx, testenv.NewLogger(t), buildWithRenewal, time.Second)
			require.Error(t, err)
			require.ErrorIs(t, rt.failure(), tc.err)
			got := memberClientFailure(ctx, testenv.NewLogger(t), rt, memberDial{build: buildWithRenewal, clientCredential: true}, metaMember{slug: "fixture"}, err)
			require.Contains(t, got.Error(), tc.want)
			require.NotContains(t, got.Error(), "reconnect")
		})
	}
}
