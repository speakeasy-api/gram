package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// roleAdapters is a plain adapter and one role adapter per role, all on one
// client, the way processes run until a role gets its own instance.
type roleAdapters struct {
	plain     *cache.RedisCacheAdapter
	auth      *cache.RoleRedisAdapter
	ops       *cache.RoleRedisAdapter
	evictable *cache.EvictableRedisAdapter
}

func newRoleAdapters(t *testing.T, mode redisrole.GuardMode) roleAdapters {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), mode)
	return roleAdapters{
		plain:     cache.NewRedisCacheAdapter(client),
		auth:      cache.NewRoleRedisAdapter(client, redisrole.RoleAuth, guard),
		ops:       cache.NewRoleRedisAdapter(client, redisrole.RoleOps, guard),
		evictable: cache.NewEvictableRedisAdapter(redisrole.EvictableClient{Client: client}, guard),
	}
}

func (a roleAdapters) of(role redisrole.Role) roleCache {
	switch role {
	case redisrole.RoleAuth:
		return a.auth
	case redisrole.RoleOps:
		return a.ops
	default:
		return a.evictable
	}
}

// roleCache is the method set every role adapter offers.
type roleCache interface {
	cache.Cache
	cache.CompareAndSwapCache
	cache.ConditionalCache
	cache.RenewableLeaseCache
	ClaimLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	ReleaseLease(ctx context.Context, key, owner string) error
	ScanKeys(ctx context.Context, prefix string) ([]string, error)
	Mutate(ctx context.Context, key string, value any, ttl time.Duration, fn func(exists bool) error) error
}

// sampleKeys is one sample key for every declared prefix of role.
func sampleKeys(role redisrole.Role) []string {
	prefixes := redisrole.Prefixes(role)
	keys := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		keys = append(keys, prefix+"sample:")
	}
	return keys
}

func TestRoleAdaptersShareDataWithPlainAdapterOnOneClient(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	ctx := t.Context()

	for _, role := range redisrole.Roles() {
		adapter := adapters.of(role)
		for _, key := range sampleKeys(role) {
			require.NoError(t, adapter.Set(ctx, key, "from-role", time.Minute), key)
			var got string
			require.NoError(t, adapters.plain.Get(ctx, key, &got), key)
			require.Equal(t, "from-role", got)

			require.NoError(t, adapters.plain.Set(ctx, key, "from-plain", time.Minute), key)
			require.NoError(t, adapter.Get(ctx, key, &got), key)
			require.Equal(t, "from-plain", got)

			require.NoError(t, adapter.Delete(ctx, key), key)
			require.Error(t, adapters.plain.Get(ctx, key, &got), key)
		}
	}
}

// roleOperations calls every adapter method with key and returns each error
// by method name.
func roleOperations(ctx context.Context, adapter roleCache, key string) map[string]error {
	errs := map[string]error{}
	var value string
	errs["Set"] = adapter.Set(ctx, key, "v", time.Minute)
	errs["Get"] = adapter.Get(ctx, key, &value)
	errs["Update"] = adapter.Update(ctx, key, "v2")
	errs["Expire"] = adapter.Expire(ctx, key, time.Minute)
	_, errs["CompareAndSwap"] = adapter.CompareAndSwap(ctx, key, "v2", "v3", time.Minute)
	_, errs["CompareAndDelete"] = adapter.CompareAndDelete(ctx, key, "v3")
	_, errs["SetIfAbsent"] = adapter.SetIfAbsent(ctx, key, "v", time.Minute)
	errs["GetAndDelete"] = adapter.GetAndDelete(ctx, key, &value)
	_, errs["Add"] = adapter.Add(ctx, key, time.Minute)
	errs["Delete"] = adapter.Delete(ctx, key)
	_, errs["AcquireLease"] = adapter.AcquireLease(ctx, key, "owner", time.Minute)
	_, errs["RenewLease"] = adapter.RenewLease(ctx, key, "owner", time.Minute)
	_, errs["ReleaseLeaseIfOwner"] = adapter.ReleaseLeaseIfOwner(ctx, key, "owner")
	_, errs["ClaimLease"] = adapter.ClaimLease(ctx, key, "owner", time.Minute)
	errs["ReleaseLease"] = adapter.ReleaseLease(ctx, key, "owner")
	errs["Mutate"] = adapter.Mutate(ctx, key, &value, time.Minute, func(bool) error { value = "m"; return nil })
	errs["Delete2"] = adapter.Delete(ctx, key)
	errs["ListAppend"] = adapter.ListAppend(ctx, key+"list", map[string]string{"item": "value"}, time.Minute)
	var items []map[string]string
	errs["ListRange"] = adapter.ListRange(ctx, key+"list", 0, -1, &items)
	_, errs["ScanKeys"] = adapter.ScanKeys(ctx, key)
	errs["DeleteByPrefix"] = adapter.DeleteByPrefix(ctx, key)
	return errs
}

func TestRoleAdaptersAllowTheirOwnKeysInEnforce(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	for _, role := range redisrole.Roles() {
		for _, key := range sampleKeys(role) {
			for method, err := range roleOperations(t.Context(), adapters.of(role), key) {
				require.NoError(t, err, "%s on %q through %s", method, key, role)
			}
		}
	}
}

func TestRoleAdaptersRefuseOtherRolesKeysInEnforce(t *testing.T) {
	t.Parallel()

	samples := map[redisrole.Role]string{
		redisrole.RoleAuth:      "sessions:v2:sid:",
		redisrole.RoleOps:       "hook:pending:sid",
		redisrole.RoleEvictable: "toolset:a:b:1:",
	}
	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	for _, role := range redisrole.Roles() {
		for keyRole, key := range samples {
			if keyRole == role {
				continue
			}
			for method, err := range roleOperations(t.Context(), adapters.of(role), key) {
				require.ErrorIs(t, err, redisrole.ErrWrongRole, "%s of %s key through %s", method, keyRole, role)
			}
		}
	}
}

func TestRoleAdaptersRefuseUnclassifiedKeysInEnforce(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	for _, role := range redisrole.Roles() {
		for method, err := range roleOperations(t.Context(), adapters.of(role), "undeclared-family:x") {
			require.ErrorIs(t, err, redisrole.ErrWrongRole, "%s through %s", method, role)
		}
	}
}

func TestRoleAdapterLetsWrongKeysThroughInReport(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardReport)
	require.NoError(t, adapters.evictable.Set(t.Context(), "sessions:v2:sid:", "v", time.Minute))
	require.NoError(t, adapters.ops.Set(t.Context(), "undeclared-family:x", "u", time.Minute))

	var got string
	require.NoError(t, adapters.plain.Get(t.Context(), "sessions:v2:sid:", &got))
	require.Equal(t, "v", got)
	require.NoError(t, adapters.plain.Get(t.Context(), "undeclared-family:x", &got))
	require.Equal(t, "u", got)
}

func TestRoleAdaptersSupportOptionalInterfaces(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	for _, store := range []cache.Cache{adapters.auth, adapters.ops, adapters.evictable} {
		_, ok := store.(cache.LeaseCache)
		require.True(t, ok)
		_, ok = store.(cache.CompareAndSwapCache)
		require.True(t, ok)
		_, ok = store.(cache.ConditionalCache)
		require.True(t, ok)
	}

	_, ok := cache.Cache(adapters.auth).(cache.MarkingConsumeCache)
	require.True(t, ok)
	_, ok = cache.Cache(adapters.evictable).(cache.MarkingConsumeCache)
	require.False(t, ok, "marking consume sets an auth-role marker, so the evictable adapter must not offer it")
	_, ok = cache.Cache(adapters.auth).(cache.EvictableCache)
	require.False(t, ok, "only the evictable adapter may be handed to evictable families")
}

func TestAuthAdapterMarkingConsumeChecksBothKeys(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	ctx := t.Context()

	require.NoError(t, adapters.auth.Set(ctx, "authnChallenge:id:", "v", time.Minute))
	var got string
	require.NoError(t, adapters.auth.GetAndDeleteMarking(ctx, "authnChallenge:id:", "connectionAttemptEnded:a", time.Minute, &got))
	require.Equal(t, "v", got)

	require.NoError(t, adapters.auth.Set(ctx, "authnChallenge:id2:", "v", time.Minute))
	err := adapters.auth.GetAndDeleteMarking(ctx, "authnChallenge:id2:", "connectionAttemptMembers:a", time.Minute, &got)
	require.ErrorIs(t, err, redisrole.ErrWrongRole, "the marker is an ops key")
}

// TestTypedGetAndDeleteMarkingOnEvictableAdapterOnlyDeletes pins the typed
// cache's fallback: without MarkingConsumeCache it consumes the key and sets
// no marker.
func TestTypedGetAndDeleteMarkingOnEvictableAdapterOnlyDeletes(t *testing.T) {
	t.Parallel()

	adapters := newRoleAdapters(t, redisrole.GuardEnforce)
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), adapters.evictable, cache.SuffixNone)
	require.NoError(t, objects.Store(t.Context(), conditionalTestObject{Key: "toolset:a", Value: "v"}))

	got, err := objects.GetAndDeleteMarking(t.Context(), "toolset:a", "connectionAttemptEnded:a", time.Minute)
	require.NoError(t, err)
	require.Equal(t, "v", got.Value)

	var marker string
	require.Error(t, adapters.plain.Get(t.Context(), "connectionAttemptEnded:a", &marker))
	_, err = objects.Get(t.Context(), "toolset:a")
	require.Error(t, err)
}
