package gram

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testSessionsKey  = "sessions:v2:sid:"
	testEvictableKey = "toolset:a:b:1:"
)

func newTestRedisStores(t *testing.T, opts redisClientOptions) (*redisStores, error) {
	t.Helper()

	stores, err := newRedisStores(t.Context(), testenv.NewLogger(t), testenv.NewMeterProvider(t), opts)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		require.NoError(t, stores.Sessions.Close())
		if stores.Evictable.Client != stores.Sessions {
			require.NoError(t, stores.Evictable.Close())
		}
	})
	return stores, nil
}

func TestSplitsEvictableRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessions  string
		evictable string
		want      bool
	}{
		{name: "unset", sessions: "redis:6379", evictable: "", want: false},
		{name: "blank", sessions: "redis:6379", evictable: "   ", want: false},
		{name: "same address", sessions: "redis:6379", evictable: "redis:6379", want: false},
		{name: "same address with spaces", sessions: "redis:6379", evictable: " redis:6379 ", want: false},
		{name: "same address with spaces on the sessions side", sessions: " redis:6379\n", evictable: "redis:6379", want: false},
		{name: "different address", sessions: "redis:6379", evictable: "evictable:6379", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, splitsEvictableRole(tc.sessions, tc.evictable))
		})
	}
}

// TestNewRedisStoresWithoutSeparateAddressUsesOneClient pins that an unset
// evictable address and one equal to the sessions address behave the same:
// one client, no guard hook, every key on the one instance. Rolling back a
// split relies on the equal case.
func TestNewRedisStoresWithoutSeparateAddressUsesOneClient(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	for name, evictableAddr := range map[string]string{
		"unset":                   "",
		"blank":                   "  ",
		"sessions address":        mr.Addr(),
		"padded sessions address": " " + mr.Addr() + " ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stores, err := newTestRedisStores(t, redisClientOptions{redisAddr: mr.Addr(), redisPassword: "", enableTracing: false, evictableAddr: evictableAddr, evictablePassword: "", guardMode: string(redisrole.GuardEnforce)})
			require.NoError(t, err)
			require.Same(t, stores.Sessions, stores.Evictable.Client)

			// No hook: the sessions client accepts an evictable key and the
			// reverse, as it does without the split.
			require.NoError(t, stores.Sessions.Set(t.Context(), testEvictableKey+name, "v", time.Minute).Err())
			require.NoError(t, stores.Evictable.Set(t.Context(), testSessionsKey+name, "v", time.Minute).Err())
			require.NoError(t, stores.SessionsCache.Set(t.Context(), testSessionsKey+name+":cache", "v", time.Minute))
			require.NoError(t, stores.EvictableCache.Set(t.Context(), testEvictableKey+name+":cache", "v", time.Minute))
			require.True(t, mr.Exists(testEvictableKey+name))
			require.True(t, mr.Exists(testSessionsKey+name))
			require.True(t, mr.Exists(testSessionsKey+name+":cache"))
			require.True(t, mr.Exists(testEvictableKey+name+":cache"))
		})
	}
}

func TestNewRedisStoresWithSeparateAddressSplitsRoles(t *testing.T) {
	t.Parallel()

	sessionsRedis := miniredis.RunT(t)
	evictableRedis := miniredis.RunT(t)
	stores, err := newTestRedisStores(t, redisClientOptions{redisAddr: sessionsRedis.Addr(), redisPassword: "", enableTracing: false, evictableAddr: evictableRedis.Addr(), evictablePassword: "", guardMode: string(redisrole.GuardEnforce)})
	require.NoError(t, err)
	require.NotSame(t, stores.Sessions, stores.Evictable.Client)

	require.NoError(t, stores.SessionsCache.Set(t.Context(), testSessionsKey, "v", time.Minute))
	require.NoError(t, stores.EvictableCache.Set(t.Context(), testEvictableKey, "v", time.Minute))
	require.True(t, sessionsRedis.Exists(testSessionsKey))
	require.False(t, evictableRedis.Exists(testSessionsKey))
	require.True(t, evictableRedis.Exists(testEvictableKey))
	require.False(t, sessionsRedis.Exists(testEvictableKey))

	require.ErrorIs(t, stores.Sessions.Set(t.Context(), testEvictableKey, "v", time.Minute).Err(), redisrole.ErrWrongRole)
	require.ErrorIs(t, stores.Evictable.Set(t.Context(), testSessionsKey, "v", time.Minute).Err(), redisrole.ErrWrongRole)
	require.ErrorIs(t, stores.EvictableCache.Set(t.Context(), testSessionsKey, "v", time.Minute), redisrole.ErrWrongRole)
}

func TestNewRedisStoresReportModeLetsWrongRoleKeysThrough(t *testing.T) {
	t.Parallel()

	sessionsRedis := miniredis.RunT(t)
	evictableRedis := miniredis.RunT(t)
	stores, err := newTestRedisStores(t, redisClientOptions{redisAddr: sessionsRedis.Addr(), redisPassword: "", enableTracing: false, evictableAddr: evictableRedis.Addr(), evictablePassword: "", guardMode: ""})
	require.NoError(t, err)

	require.NoError(t, stores.Sessions.Set(t.Context(), testEvictableKey, "v", time.Minute).Err())
	require.True(t, sessionsRedis.Exists(testEvictableKey))
}

func TestNewRedisStoresFailsOnUnreachableEvictableAddress(t *testing.T) {
	t.Parallel()

	sessionsRedis := miniredis.RunT(t)
	unreachable := miniredis.RunT(t)
	addr := unreachable.Addr()
	unreachable.Close()

	_, err := newTestRedisStores(t, redisClientOptions{redisAddr: sessionsRedis.Addr(), redisPassword: "", enableTracing: false, evictableAddr: addr, evictablePassword: "", guardMode: ""})
	require.Error(t, err)
}

func TestNewRedisStoresRejectsUnknownGuardMode(t *testing.T) {
	t.Parallel()

	sessionsRedis := miniredis.RunT(t)
	_, err := newTestRedisStores(t, redisClientOptions{redisAddr: sessionsRedis.Addr(), redisPassword: "", enableTracing: false, evictableAddr: "", evictablePassword: "", guardMode: "off"})
	require.Error(t, err)
}
