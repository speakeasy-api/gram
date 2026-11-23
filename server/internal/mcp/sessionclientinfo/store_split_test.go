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

// TestStoreOnSplitInstancesLeavesSessionsEmpty runs both scripts (admit with
// an eviction, and load) on a separate evictable instance whose client refuses
// any sessions-role key, and checks that the sessions instance stays empty.
func TestStoreOnSplitInstancesLeavesSessionsEmpty(t *testing.T) {
	t.Parallel()

	sessionsRedis := miniredis.RunT(t)
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

	require.Empty(t, sessionsRedis.Keys())
	require.Len(t, evictableRedis.Keys(), 2, "the live set and the surviving record")
}
