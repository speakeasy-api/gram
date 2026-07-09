package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestForwardRequestWithRetryClosesBodyOnRetryerError: when the retryer itself
// errors (e.g. Redis down during Unpublish/Candidates), the first upstream
// response must be closed and NOT returned — callers bail on err before they
// register their Body.Close defer, so an open response here pins the upstream
// connection until the phase timer fires.
func TestForwardRequestWithRetryClosesBodyOnRetryerError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Gram-Tunnel-Error", "no-live-session")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`bad gateway`))
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	retryerErr := errors.New("list tunnel retry routes: redis unavailable")
	p := &Proxy{
		GuardianPolicy:        policy,
		GuardianClientOptions: nil,
		Logger:                testenv.NewLogger(t),
		Tracer:                testenv.NewTracerProvider(t).Tracer("test"),
		NonStreamingTimeout:   5 * time.Second,
		StreamingTimeout:      5 * time.Second,
		Metrics:               nil,
		MaxBufferedBodyBytes:  DefaultMaxBufferedBodyBytes,
		Identity: ServerIdentity{
			RemoteMCPServerID:   "",
			TunneledMCPServerID: "tunnel-1",
			McpServerID:         "",
		},
		RemoteURL:             upstream.URL,
		Headers:               nil,
		AuthorizationOverride: "",
		UpstreamResponseRetryer: func(_ context.Context, _ *http.Response) (*UpstreamResponseRetry, error) {
			return nil, retryerErr
		},
		UserRequestInterceptors:           nil,
		InitializeRequestInterceptors:     nil,
		RemoteMessageInterceptors:         nil,
		ToolsCallRequestInterceptors:      nil,
		ToolsCallResponseInterceptors:     nil,
		ToolsListRequestInterceptors:      nil,
		ToolsListResponseInterceptors:     nil,
		ResourcesReadRequestInterceptors:  nil,
		ResourcesReadResponseInterceptors: nil,
		ResourcesListRequestInterceptors:  nil,
		ResourcesListResponseInterceptors: nil,
	}

	req := httptest.NewRequest(http.MethodPost, "http://gram.local/mcp", nil)
	upstreamReq, upstreamResp, err := p.forwardRequestWithRetry(t.Context(), req, func() io.Reader { return nil })
	if upstreamResp != nil {
		// Unreachable when the contract holds; guards the leak if it regresses.
		defer func() { _ = upstreamResp.Body.Close() }()
	}

	require.ErrorIs(t, err, retryerErr)
	require.NotNil(t, upstreamReq)
	require.Nil(t, upstreamResp, "an open response must not be returned alongside a retryer error")
}
