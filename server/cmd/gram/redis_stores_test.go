package gram

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestRedisStoresShareOneClient pins the wiring's runtime behaviour: every
// role uses the one client a process had before the roles existed, so each
// command goes to the same instance over the same pool.
func TestRedisStoresShareOneClient(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	stores, err := newRedisStores(t.Context(), testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: mr.Addr(), redisPassword: "", environment: "", enableTracing: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })

	require.Same(t, stores.Auth, stores.Ops)
	require.Same(t, stores.Auth, stores.Evictable.Client)
	require.Equal(t, redisrole.RoleAuth, stores.AuthCache.Role())
	require.Equal(t, redisrole.RoleOps, stores.OpsCache.Role())

	ctx := t.Context()
	require.NoError(t, stores.AuthCache.Set(ctx, "sessions:v2:sid:", "a", time.Minute))
	require.NoError(t, stores.OpsCache.Set(ctx, "hook:pending:sid", "o", time.Minute))
	require.NoError(t, stores.EvictableCache.Set(ctx, "toolset:a:b:1:", "e", time.Minute))
	require.ElementsMatch(t, []string{"sessions:v2:sid:", "hook:pending:sid", "toolset:a:b:1:"}, mr.Keys(), "every role writes to the one instance")
}

// TestRedisStoresCloseClosesTheSharedClientOnce closes a shared client once,
// so shutdown reports no "client is closed" error.
func TestRedisStoresCloseClosesTheSharedClientOnce(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	stores, err := newRedisStores(t.Context(), testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: mr.Addr(), redisPassword: "", environment: "", enableTracing: false})
	require.NoError(t, err)

	require.NoError(t, stores.Close())
	require.Error(t, stores.Auth.Ping(t.Context()).Err())
}

func setRoleEndpoint(t *testing.T, prefix, addr string) {
	t.Helper()
	t.Setenv(prefix+"ADDR", addr)
	t.Setenv(prefix+"PASSWORD", "")
	t.Setenv(prefix+"CA_CERT", "")
	t.Setenv(prefix+"AUTH_MODE", "none")
}

func TestRedisStoresUseRoleEndpointsWithoutSessionsAddress(t *testing.T) { //nolint:paralleltest // sets environment variables
	auth := miniredis.RunT(t)
	ops := miniredis.RunT(t)
	evictable := miniredis.RunT(t)
	setRoleEndpoint(t, "GRAM_REDIS_AUTH_", auth.Addr())
	setRoleEndpoint(t, "GRAM_REDIS_OPS_", ops.Addr())
	setRoleEndpoint(t, "GRAM_REDIS_EVICTABLE_", evictable.Addr())

	ctx := t.Context()
	stores, err := newRedisStores(ctx, testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: "", redisPassword: "", environment: "", enableTracing: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })

	require.NoError(t, stores.AuthCache.Set(ctx, "sessions:v2:sid:", "a", time.Minute))
	require.NoError(t, stores.OpsCache.Set(ctx, "hook:pending:sid", "o", time.Minute))
	require.NoError(t, stores.EvictableCache.Set(ctx, "toolset:a:b:1:", "e", time.Minute))
	require.Equal(t, []string{"sessions:v2:sid:"}, auth.Keys())
	require.Equal(t, []string{"hook:pending:sid"}, ops.Keys())
	require.Equal(t, []string{"toolset:a:b:1:"}, evictable.Keys())
}

// TestRedisStoresGuardDedicatedRawClients pins the hook on a client that
// serves one role: raw users of that client (rate limits, routes, sessions)
// are checked too, and enforce mode refuses another role's key.
func TestRedisStoresGuardDedicatedRawClients(t *testing.T) {
	sessions := miniredis.RunT(t)
	ops := miniredis.RunT(t)
	t.Setenv("GRAM_REDIS_OPS_ADDR", ops.Addr())
	t.Setenv("GRAM_REDIS_OPS_AUTH_MODE", "none")
	t.Setenv("GRAM_REDIS_OPS_PASSWORD", "")
	t.Setenv("GRAM_REDIS_OPS_CA_CERT", "")
	t.Setenv("GRAM_REDIS_ROLE_GUARD", "enforce")

	ctx := t.Context()
	stores, err := newRedisStores(ctx, testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: sessions.Addr(), redisPassword: "", environment: "", enableTracing: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })

	require.NoError(t, stores.Ops.Set(ctx, "hook:pending:sid", "o", time.Minute).Err())
	require.ErrorIs(t, stores.Ops.Set(ctx, "sessions:v2:sid:", "a", time.Minute).Err(), redisrole.ErrWrongRole)
	require.Equal(t, []string{"hook:pending:sid"}, ops.Keys())

	// Auth and Evictable still share the Sessions client, which cannot tell
	// them apart, so it carries no hook.
	require.Same(t, stores.Auth, stores.Evictable.Client)
	require.NoError(t, stores.Auth.Set(ctx, "toolset:a:b:1:", "e", time.Minute).Err())
}

func TestRedisStoresReuseSessionsForARoleAddressEqualToIt(t *testing.T) {
	sessions := miniredis.RunT(t)
	sessions.RequireAuth("sessions-password")
	t.Setenv("GRAM_REDIS_EVICTABLE_ADDR", sessions.Addr())
	t.Setenv("GRAM_REDIS_EVICTABLE_AUTH_MODE", "iam")
	t.Setenv("GRAM_REDIS_EVICTABLE_CA_CERT", "stale Valkey CA")

	ctx := t.Context()
	stores, err := newRedisStores(ctx, testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: sessions.Addr(), redisPassword: "sessions-password", environment: "", enableTracing: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })

	require.Same(t, stores.Auth, stores.Ops)
	require.Same(t, stores.Auth, stores.Evictable.Client)
}

// TestRedisStoresIgnoreNextEndpoints pins that the instance switch is gone: a
// leftover next address neither moves a role nor makes startup touch it.
func TestRedisStoresIgnoreNextEndpoints(t *testing.T) {
	auth := miniredis.RunT(t)
	ops := miniredis.RunT(t)
	setRoleEndpoint(t, "GRAM_REDIS_AUTH_", auth.Addr())
	setRoleEndpoint(t, "GRAM_REDIS_OPS_", ops.Addr())
	setRoleEndpoint(t, "GRAM_REDIS_EVICTABLE_", ops.Addr())
	t.Setenv("GRAM_REDIS_AUTH_NEXT_ADDR", "127.0.0.1:1")
	t.Setenv("GRAM_REDIS_OPS_NEXT_ADDR", "127.0.0.1:1")
	t.Setenv("GRAM_REDIS_NEXT_ADDR", "127.0.0.1:1")

	ctx := t.Context()
	stores, err := newRedisStores(ctx, testenv.NewLogger(t), testenv.NewMeterProvider(t), redisClientOptions{redisAddr: "", redisPassword: "", environment: "", enableTracing: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stores.Close()) })

	require.NoError(t, stores.AuthCache.Set(ctx, "sessions:v2:sid:", "a", time.Minute))
	require.NoError(t, stores.OpsCache.Set(ctx, "hook:pending:sid", "o", time.Minute))
	require.Equal(t, []string{"sessions:v2:sid:"}, auth.Keys())
	require.Equal(t, []string{"hook:pending:sid"}, ops.Keys())
}
