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
		UserRequestObservationInterceptors: nil,
		UserRequestInterceptors:            nil,
		InitializeRequestInterceptors:      nil,
		RemoteMessageInterceptors:          nil,
		ToolsCallRequestInterceptors:       nil,
		ToolsCallResponseInterceptors:      nil,
		ToolsListRequestInterceptors:       nil,
		ToolsListResponseInterceptors:      nil,
		ResourcesReadRequestInterceptors:   nil,
		ResourcesReadResponseInterceptors:  nil,
		ResourcesListRequestInterceptors:   nil,
		ResourcesListResponseInterceptors:  nil,
	}

	req := httptest.NewRequest(http.MethodPost, "http://gram.local/mcp", nil)
	upstreamReq, upstreamResp, err := p.forwardRequestWithRetry(t.Context(), req, func() io.Reader { return nil }, nil)
	if upstreamResp != nil {
		// Unreachable when the contract holds; guards the leak if it regresses.
		defer func() { _ = upstreamResp.Body.Close() }()
	}

	require.ErrorIs(t, err, retryerErr)
	require.NotNil(t, upstreamReq)
	require.Nil(t, upstreamResp, "an open response must not be returned alongside a retryer error")
}

// TestForwardRequestWithRetryReplacesOnlyAuthorization: a retry that names
// only a new bearer token resends the request to the same upstream with the
// same configured headers.
func TestForwardRequestWithRetryReplacesOnlyAuthorization(t *testing.T) {
	t.Parallel()

	type received struct {
		authorization string
		configured    string
	}
	seen := make(chan received, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- received{authorization: r.Header.Get("Authorization"), configured: r.Header.Get("X-Configured")}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	p := &Proxy{
		GuardianPolicy:        policy,
		Logger:                testenv.NewLogger(t),
		Tracer:                testenv.NewTracerProvider(t).Tracer("test"),
		NonStreamingTimeout:   5 * time.Second,
		StreamingTimeout:      5 * time.Second,
		MaxBufferedBodyBytes:  DefaultMaxBufferedBodyBytes,
		RemoteURL:             upstream.URL,
		Headers:               []ConfiguredHeader{{Name: "X-Configured", StaticValue: "kept"}},
		AuthorizationOverride: "stale",
		UpstreamResponseRetryer: func(_ context.Context, resp *http.Response) (*UpstreamResponseRetry, error) {
			if resp.StatusCode != http.StatusUnauthorized {
				return nil, nil
			}
			return &UpstreamResponseRetry{AuthorizationOverride: "fresh"}, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "http://gram.local/mcp", nil)
	_, upstreamResp, err := p.forwardRequestWithRetry(t.Context(), req, func() io.Reader { return nil }, nil)
	require.NoError(t, err)
	defer func() { _ = upstreamResp.Body.Close() }()

	require.Equal(t, http.StatusOK, upstreamResp.StatusCode)
	require.Equal(t, received{authorization: "Bearer stale", configured: "kept"}, <-seen)
	require.Equal(t, received{authorization: "Bearer fresh", configured: "kept"}, <-seen)
	require.Equal(t, upstream.URL, p.RemoteURL)
}

func TestChainUpstreamResponseRetryers_FirstRetryWins(t *testing.T) {
	t.Parallel()

	laterCalled := false
	chained := ChainUpstreamResponseRetryers(
		nil,
		func(context.Context, *http.Response) (*UpstreamResponseRetry, error) {
			return &UpstreamResponseRetry{RemoteURL: "http://first.test"}, nil
		},
		func(context.Context, *http.Response) (*UpstreamResponseRetry, error) {
			laterCalled = true
			return &UpstreamResponseRetry{RemoteURL: "http://second.test"}, nil
		},
	)

	retry, err := chained(t.Context(), &http.Response{StatusCode: http.StatusBadGateway})
	require.NoError(t, err)
	require.Equal(t, "http://first.test", retry.RemoteURL)
	require.False(t, laterCalled)
}

func TestChainUpstreamResponseRetryers_FallsThroughKeptResponse(t *testing.T) {
	t.Parallel()

	chained := ChainUpstreamResponseRetryers(
		func(context.Context, *http.Response) (*UpstreamResponseRetry, error) { return nil, nil },
		func(context.Context, *http.Response) (*UpstreamResponseRetry, error) {
			return &UpstreamResponseRetry{AuthorizationOverride: "fresh"}, nil
		},
	)

	retry, err := chained(t.Context(), &http.Response{StatusCode: http.StatusUnauthorized})
	require.NoError(t, err)
	require.Equal(t, "fresh", retry.AuthorizationOverride)
}

func TestChainUpstreamResponseRetryers_StopsOnError(t *testing.T) {
	t.Parallel()

	failure := errors.New("retry lookup failed")
	laterCalled := false
	chained := ChainUpstreamResponseRetryers(
		func(context.Context, *http.Response) (*UpstreamResponseRetry, error) { return nil, failure },
		func(context.Context, *http.Response) (*UpstreamResponseRetry, error) {
			laterCalled = true
			return nil, nil
		},
	)

	_, err := chained(t.Context(), &http.Response{StatusCode: http.StatusUnauthorized})
	require.ErrorIs(t, err, failure)
	require.False(t, laterCalled)
}
