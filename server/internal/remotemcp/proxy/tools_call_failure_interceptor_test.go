package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// mockToolsCallFailureInterceptor is a [proxy.ToolsCallFailureInterceptor]
// that records every failure it is told about.
type mockToolsCallFailureInterceptor struct {
	failures []*proxy.ToolsCallFailure
}

func (m *mockToolsCallFailureInterceptor) Name() string { return "failure" }

func (m *mockToolsCallFailureInterceptor) InterceptToolsCallFailure(_ context.Context, failure *proxy.ToolsCallFailure) {
	m.failures = append(m.failures, failure)
}

func TestProxy_Post_ToolsCallFailureInterceptor(t *testing.T) {
	t.Parallel()

	jsonReply := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}
	}

	tests := []struct {
		name     string
		upstream http.HandlerFunc
		// unreachable points the proxy at a closed port instead.
		unreachable bool
		configure   func(*proxy.Proxy)
		request     string
		canceled    bool

		wantResponse bool
		wantFailure  bool
		wantStatus   int
		wantErrCode  oops.Code
	}{
		{
			name:         "result",
			upstream:     jsonReply(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`),
			wantResponse: true,
		},
		{
			name:         "tool error result",
			upstream:     jsonReply(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":true}}`),
			wantResponse: true,
		},
		{
			name:         "json-rpc error",
			upstream:     jsonReply(http.StatusOK, `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"boom"}}`),
			wantResponse: true,
		},
		{
			// The tunnel agent answers this way when it cannot reach its target.
			name: "error status with empty body",
			upstream: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			wantFailure: true,
			wantStatus:  http.StatusBadGateway,
		},
		{
			name: "error status with plain-text body",
			upstream: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			},
			wantFailure: true,
			wantStatus:  http.StatusServiceUnavailable,
		},
		{
			name:        "2xx body that is not JSON-RPC",
			upstream:    jsonReply(http.StatusOK, `{"Output":"raw"}`),
			wantFailure: true,
			wantStatus:  http.StatusOK,
		},
		{
			name: "stream that ends before the reply",
			upstream: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, sseBody(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.5}}`))
			},
			wantFailure: true,
			wantStatus:  http.StatusOK,
		},
		{
			name: "upstream response refused",
			upstream: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			configure: func(p *proxy.Proxy) {
				p.UpstreamResponseInterceptor = func(context.Context, *http.Response) error {
					return &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "busy", Data: nil}
				}
			},
			wantFailure: true,
			wantStatus:  http.StatusBadGateway,
		},
		{
			name:        "unreachable upstream",
			unreachable: true,
			wantFailure: true,
			wantStatus:  0,
			wantErrCode: oops.CodeGatewayError,
		},
		{
			name:     "request rejected before forwarding",
			upstream: jsonReply(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`),
			configure: func(p *proxy.Proxy) {
				p.ToolsCallRequestInterceptors = []proxy.ToolsCallRequestInterceptor{
					&mockToolsCallRequestInterceptor{name: "deny", err: &proxy.RejectError{Code: proxy.RejectCodeServerError, Message: "denied", Data: nil}},
				}
			},
		},
		{
			name:     "user canceled",
			upstream: jsonReply(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`),
			canceled: true,
		},
		{
			name: "notification",
			upstream: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			},
			request: `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_weather","arguments":{}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(tc.upstream)
			upstreamURL := upstream.URL
			if tc.unreachable {
				upstream.Close()
			} else {
				t.Cleanup(upstream.Close)
			}

			var responses int
			failures := &mockToolsCallFailureInterceptor{}
			p := newProxyForTest(t, upstreamURL)
			p.NonStreamingTimeout = 2 * time.Second
			p.ToolsCallResponseInterceptors = []proxy.ToolsCallResponseInterceptor{
				&mockToolsCallResponseInterceptor{name: "typed", called: &responses},
			}
			p.ToolsCallFailureInterceptors = []proxy.ToolsCallFailureInterceptor{failures}
			if tc.configure != nil {
				tc.configure(p)
			}

			body := tc.request
			if body == "" {
				body = toolsCallRequest
			}
			ctx := t.Context()
			if tc.canceled {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/id", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")

			err := p.Post(httptest.NewRecorder(), req)

			require.Equal(t, tc.wantResponse, responses == 1, "response interceptor invocation")
			if !tc.wantFailure {
				require.Empty(t, failures.failures, "failure interceptor must not run")
				return
			}
			require.Len(t, failures.failures, 1, "failure interceptor must run exactly once")
			failure := failures.failures[0]
			require.NotNil(t, failure.Request)
			require.Equal(t, "get_weather", failure.Request.Params.Name)
			require.Equal(t, tc.wantStatus, failure.StatusCode)
			require.Equal(t, err, failure.Err, "failure carries the error the proxy returned")
			if tc.wantErrCode != "" {
				var shareable *oops.ShareableError
				require.ErrorAs(t, failure.Err, &shareable)
				require.Equal(t, tc.wantErrCode, shareable.Code)
			}
		})
	}
}
