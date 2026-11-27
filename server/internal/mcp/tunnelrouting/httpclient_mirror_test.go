package tunnelrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
)

// heldRoutes blocks every command until its context ends, like an operations
// instance held by an instance move, and counts the commands it was sent.
type heldRoutes struct {
	commands atomic.Int32
}

func (h *heldRoutes) Publish(ctx context.Context, _, _ string, _ time.Duration) error {
	h.commands.Add(1)
	<-ctx.Done()
	return fmt.Errorf("operations held: %w", ctx.Err())
}

func (h *heldRoutes) Candidates(ctx context.Context, _ string) ([]string, error) {
	h.commands.Add(1)
	<-ctx.Done()
	return nil, fmt.Errorf("operations held: %w", ctx.Err())
}

func (h *heldRoutes) Unpublish(ctx context.Context, _, _ string) error {
	h.commands.Add(1)
	<-ctx.Done()
	return fmt.Errorf("operations held: %w", ctx.Err())
}

func (h *heldRoutes) Delete(ctx context.Context, _ string) error {
	h.commands.Add(1)
	<-ctx.Done()
	return fmt.Errorf("operations held: %w", ctx.Err())
}

// downOpsRoutes returns the bulk route store on an operations instance that
// no longer accepts connections.
func downOpsRoutes(t *testing.T) route.Store {
	t.Helper()
	ops := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: ops.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	ops.Close()
	return route.NewRedis(client)
}

// mirroredRoute returns the auth role's mirror holding one route for tunnelID.
func mirroredRoute(t *testing.T, tunnelID, addr string) route.Store {
	t.Helper()
	auth := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: auth.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	mirror := route.NewAuthMirror(client)
	require.NoError(t, mirror.Publish(t.Context(), tunnelID, addr, 30*time.Second))
	return mirror
}

func okGateway(t *testing.T) *httptest.Server {
	t.Helper()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(gateway.Close)
	return gateway
}

func TestHTTPClientWithoutMirrorFailsWhileOperationsIsDown(t *testing.T) {
	t.Parallel()

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, downOpsRoutes(t)).Do(req, "tunnel-1") //nolint:bodyclose // error path returns a nil response
	require.ErrorIs(t, err, ErrRouteStoreUnavailable)
	require.Nil(t, resp)
}

func TestHTTPClientReachesMirroredTunnelWhileOperationsIsDown(t *testing.T) {
	t.Parallel()

	gateway := okGateway(t)
	routes := route.NewMirrorReader(mirroredRoute(t, "tunnel-1", gateway.URL), downOpsRoutes(t))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHTTPClientReachesMirroredTunnelWhileOperationsIsHeldForAMove(t *testing.T) {
	t.Parallel()

	gateway := okGateway(t)
	ops := &heldRoutes{commands: atomic.Int32{}}
	routes := route.NewMirrorReader(mirroredRoute(t, "tunnel-1", gateway.URL), ops)

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Zero(t, ops.commands.Load(), "the held operations store was never asked")
}
