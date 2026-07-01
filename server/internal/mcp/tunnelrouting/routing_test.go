package tunnelrouting

import (
	"context"
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

	ctx := context.Background()
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

	ctx := context.Background()
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

func TestRetryerPlainBadGatewayDoesNotRetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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

	ctx := context.Background()
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
	header := make(http.Header)
	header.Set(ErrorHeader, value)
	return &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     header,
		Body:       http.NoBody,
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
