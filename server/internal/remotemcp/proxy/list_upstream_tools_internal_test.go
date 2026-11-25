package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestListUpstreamTools_ReadsEveryPageInItsOwnSession(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "upstream", Version: "1"}, &mcp.ServerOptions{PageSize: 1})
	for _, name := range []string{"one", "two", "three"} {
		server.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	var callerSessionSeen atomic.Bool
	var bearer atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(McpSessionIDHeader) == "caller-session" {
			callerSessionSeen.Store(true)
		}
		bearer.Store(r.Header.Get("Authorization"))
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	p := &Proxy{
		GuardianPolicy:        policy,
		Logger:                testenv.NewLogger(t),
		NonStreamingTimeout:   DefaultNonStreamingTimeout,
		StreamingTimeout:      DefaultStreamingTimeout,
		RemoteURL:             upstream.URL,
		AuthorizationOverride: "upstream-token",
	}
	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	userReq.Header.Set(McpSessionIDHeader, "caller-session")
	userReq.Header.Set("Authorization", "Bearer speakeasy-credential")

	tools, err := p.ListUpstreamTools(t.Context(), userReq)
	require.NoError(t, err)

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	require.ElementsMatch(t, []string{"one", "two", "three"}, names)
	require.False(t, callerSessionSeen.Load(), "the listing must not touch the caller's session")
	require.Equal(t, "Bearer upstream-token", bearer.Load(), "the listing uses the resolved upstream credential")
}

func TestListUpstreamTools_UpstreamRejectionCarriesTheChallenge(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	p := &Proxy{
		GuardianPolicy:      policy,
		Logger:              testenv.NewLogger(t),
		NonStreamingTimeout: DefaultNonStreamingTimeout,
		StreamingTimeout:    DefaultStreamingTimeout,
		RemoteURL:           upstream.URL,
	}

	_, err = p.ListUpstreamTools(t.Context(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil))
	var refusal *UpstreamListingRefusalError
	require.ErrorAs(t, err, &refusal)
	require.NotNil(t, refusal.Response)
	require.Equal(t, http.StatusUnauthorized, refusal.Response.StatusCode)
	require.Equal(t, `Bearer error="invalid_token"`, refusal.Response.Header.Get("WWW-Authenticate"))
}
