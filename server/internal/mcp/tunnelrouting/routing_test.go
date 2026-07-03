package tunnelrouting

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestClientAffinityKeyFromRequest(t *testing.T) {
	t.Parallel()

	req := func(auth, chat string) *http.Request {
		r := httptestRequest()
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		if chat != "" {
			r.Header.Set(constants.ChatSessionsTokenHeader, chat)
		}
		return r
	}

	require.Equal(t, expectedAffinity("auth-token"), ClientAffinityKeyFromRequest(req("Bearer auth-token", "chat-token")))
	require.Equal(t, expectedAffinity("raw-auth-token"), ClientAffinityKeyFromRequest(req("raw-auth-token", "chat-token")))
	require.Equal(t, expectedAffinity("chat-token"), ClientAffinityKeyFromRequest(req("", "chat-token")))
	require.Empty(t, ClientAffinityKeyFromRequest(req("", "")))
}

func TestRetryerNoLiveSessionUnpublishesAndFailsOver(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1002", time.Minute))

	resp := tunnelErrorResponse("no-live-session")
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.NotNil(t, retry)
	require.Equal(t, "http://127.0.0.1:1002", retry.RemoteURL)
	require.Equal(t, "tunnel-1", headerValue(t, retry.Headers, wire.HeaderTunnelID))
	require.Equal(t, "forward-token", headerValue(t, retry.Headers, wire.HeaderTunnelForwardToken))
	require.Equal(t, "auth:stable", headerValue(t, retry.Headers, wire.HeaderTunnelConsumerSession))

	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1:1002"}, candidates)
}

func TestRetryerSubstreamFailedRetriesSameRouteWithoutUnpublish(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))

	resp := tunnelErrorResponse("substream-failed")
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.NotNil(t, retry)
	require.Equal(t, "http://127.0.0.1:1001", retry.RemoteURL)

	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1:1001"}, candidates)
}

// TestRetryerTunnelBusyFailsOverWithoutUnpublish: tunnel-busy means the
// selected gateway is healthy but at capacity — its route must stay published
// while the request fails over to another candidate, and replay is safe for
// any method (the request never entered a substream).
func TestRetryerTunnelBusyFailsOverWithoutUnpublish(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1002", time.Minute))

	resp := tunnelErrorResponseForMethod(wire.TunnelErrorTunnelBusy, http.MethodPost)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.NotNil(t, retry)
	require.Equal(t, "http://127.0.0.1:1002", retry.RemoteURL)

	// Both routes stay published: busy is not stale.
	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"127.0.0.1:1001", "127.0.0.1:1002"}, candidates)
}

func TestRetryerTunnelBusySingleCandidateDoesNotRetry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))

	resp := tunnelErrorResponseForMethod(wire.TunnelErrorTunnelBusy, http.MethodPost)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.Nil(t, retry)

	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1:1001"}, candidates)
}

// TestRetryerSubstreamFailedDoesNotReplayPost guards against double-executing
// non-idempotent MCP calls: substream-failed can fire after the request body
// reached the backend (the substream died awaiting response headers), so the
// gateway cannot know whether a tools/call already executed. POSTs must not
// be replayed; only idempotent methods (GET/DELETE) may retry.
func TestRetryerSubstreamFailedDoesNotReplayPost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))

	resp := tunnelErrorResponseForMethod("substream-failed", http.MethodPost)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.Nil(t, retry)

	// The route stays published: the gateway is healthy, the substream broke.
	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1:1001"}, candidates)
}

// TestRetryerSubstreamFailedNilRequestDoesNotRetry: without the originating
// request we cannot prove the method was idempotent, so fail safe.
func TestRetryerSubstreamFailedNilRequestDoesNotRetry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))

	resp := tunnelErrorResponse("substream-failed")
	resp.Request = nil
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.Nil(t, retry)
}

func TestRetryerPlainBadGatewayDoesNotRetry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))

	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.Nil(t, retry)

	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"127.0.0.1:1001"}, candidates)
}

func TestRetryerNoLiveSessionSingleCandidateDoesNotRetry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, "tunnel-1", "127.0.0.1:1001", time.Minute))

	resp := tunnelErrorResponse("no-live-session")
	defer func() { require.NoError(t, resp.Body.Close()) }()
	retry, err := Retryer(routes, "tunnel-1", "127.0.0.1:1001", "auth:stable", "forward-token")(ctx, resp)
	require.NoError(t, err)
	require.Nil(t, retry)

	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func httptestRequest() *http.Request {
	req, err := http.NewRequest(http.MethodGet, "http://example.test/mcp", nil)
	if err != nil {
		panic(err)
	}
	return req
}

func tunnelErrorResponse(value string) *http.Response {
	return tunnelErrorResponseForMethod(value, http.MethodGet)
}

func tunnelErrorResponseForMethod(value, method string) *http.Response {
	header := make(http.Header)
	header.Set(ErrorHeader, value)
	req, err := http.NewRequest(method, "http://gateway.internal/forward", nil)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}
}

func headerValue(t *testing.T, headers []proxy.ConfiguredHeader, name string) string {
	t.Helper()
	for _, header := range headers {
		if header.Name == name {
			return header.StaticValue
		}
	}
	t.Fatalf("missing header %s", name)
	return ""
}

func expectedAffinity(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "auth:" + hex.EncodeToString(sum[:])
}
