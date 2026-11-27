package sessionclientinfo

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestStoreOnEvictableInstanceWritesOnlyEvictableKeys runs both scripts (admit
// with an eviction, and load) on an evictable instance whose client refuses
// any declared sessions-role key, then checks that every key left on the
// instance, including those built inside the scripts, classifies as
// evictable. A family written here that belongs to the sessions role would
// be lost to eviction once the instance is split.
func TestStoreOnEvictableInstanceWritesOnlyEvictableKeys(t *testing.T) {
	t.Parallel()

	evictableRedis := miniredis.RunT(t)
	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), redisrole.GuardEnforce)
	evictableClient := redis.NewClient(&redis.Options{Addr: evictableRedis.Addr()})
	t.Cleanup(func() { require.NoError(t, evictableClient.Close()) })
	evictableClient.AddHook(guard.Hook(redisrole.RoleEvictable))

	store := NewStore(redisrole.EvictableClient{Client: evictableClient}, 1)
	projectID := uuid.New()
	ctx := t.Context()

	require.NoError(t, store.Store(ctx, projectID, "toolset", "first", Info{Name: "first", Version: "1", ProtocolVersion: ""}, 1))
	// A cap of one evicts the first record through the key built inside the
	// admit script.
	require.NoError(t, store.Store(ctx, projectID, "toolset", "second", Info{Name: "second", Version: "1", ProtocolVersion: ""}, 2))

	info, err := store.Load(ctx, projectID, "toolset", "second", 3)
	require.NoError(t, err)
	require.Equal(t, "second", info.Name)
	_, err = store.Load(ctx, projectID, "toolset", "first", 4)
	require.ErrorIs(t, err, ErrNotFound)

	keys := evictableRedis.Keys()
	require.Len(t, keys, 2, "the live set and the surviving record")
	for _, key := range keys {
		role, _ := redisrole.Of(key)
		require.Equal(t, redisrole.RoleEvictable, role, "key %q", key)
	}
}
