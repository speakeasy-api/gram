package gram

import (
	"flag"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
)

func tunnelAuthRouteMirrorCLIContext(t *testing.T, enabled bool) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("tunnel-auth-route-mirror", flag.ContinueOnError)
	require.NoError(t, tunnelAuthRouteMirrorFlag().Apply(set))
	require.NoError(t, set.Set(tunnelAuthRouteMirrorFlagName, strconv.FormatBool(enabled)))
	return cli.NewContext(nil, set, nil)
}

// splitRoleStores builds stores whose auth and ops roles are separate
// instances, with the role guard refusing any key on the wrong one.
func splitRoleStores(t *testing.T) (*redisStores, *miniredis.Miniredis, *miniredis.Miniredis) {
	t.Helper()
	sessions := miniredis.RunT(t)
	auth := miniredis.RunT(t)
	ops := miniredis.RunT(t)
	t.Setenv("GRAM_REDIS_AUTH_ADDR", auth.Addr())
	t.Setenv("GRAM_REDIS_OPS_ADDR", ops.Addr())
	for _, prefix := range []string{"GRAM_REDIS_AUTH_", "GRAM_REDIS_OPS_"} {
		t.Setenv(prefix+"PASSWORD", "")
		t.Setenv(prefix+"CA_CERT", "")
		t.Setenv(prefix+"AUTH_MODE", "none")
		t.Setenv(prefix+"NEXT_ADDR", "")
	}
	t.Setenv("GRAM_REDIS_ROLE_GUARD", "enforce")

	stores, err := newRedisStores(t.Context(), testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: sessions.Addr(), redisPassword: "", environment: "", enableTracing: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })
	return stores, auth, ops
}

// TestTunnelOAuthRoutesIgnoreMirrorUntilEnabled keeps the default runtime
// unchanged: without the flag, OAuth calls read only the ops role's routes.
func TestTunnelOAuthRoutesIgnoreMirrorUntilEnabled(t *testing.T) { //nolint:paralleltest // splitRoleStores sets process environment
	stores, _, _ := splitRoleStores(t)
	ctx := t.Context()
	require.NoError(t, route.NewAuthMirror(stores.Auth).Publish(ctx, "tunnel-1", "mirrored-gateway", time.Minute))
	require.NoError(t, route.NewRedis(stores.Ops).Publish(ctx, "tunnel-1", "bulk-gateway", time.Minute))

	routes := tunnelOAuthRoutes(tunnelAuthRouteMirrorCLIContext(t, false), stores)
	require.IsType(t, &route.Redis{}, routes)
	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"bulk-gateway"}, candidates)
}

// TestTunnelOAuthRoutesReadMirrorWhileOperationsIsDown is tunneled OAuth's
// route lookup with the flag on and the ops instance gone.
func TestTunnelOAuthRoutesReadMirrorWhileOperationsIsDown(t *testing.T) { //nolint:paralleltest // splitRoleStores sets process environment
	stores, auth, ops := splitRoleStores(t)
	ctx := t.Context()
	require.NoError(t, route.NewAuthMirror(stores.Auth).Publish(ctx, "tunnel-1", "mirrored-gateway", time.Minute))
	require.Equal(t, []string{"auth_tunnel_routes:tunnel-1"}, auth.Keys())
	ops.Close()

	routes := tunnelOAuthRoutes(tunnelAuthRouteMirrorCLIContext(t, true), stores)
	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"mirrored-gateway"}, candidates)
}

// TestTunnelOAuthRoutesFallBackToOperations serves a tunnel the gateway does
// not mirror from the ops role's routes.
func TestTunnelOAuthRoutesFallBackToOperations(t *testing.T) { //nolint:paralleltest // splitRoleStores sets process environment
	stores, _, _ := splitRoleStores(t)
	ctx := t.Context()
	require.NoError(t, route.NewRedis(stores.Ops).Publish(ctx, "tunnel-1", "bulk-gateway", time.Minute))

	routes := tunnelOAuthRoutes(tunnelAuthRouteMirrorCLIContext(t, true), stores)
	candidates, err := routes.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"bulk-gateway"}, candidates)
}
