package route

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRouteTableUnpublishLeavesOtherOwners(t *testing.T) {
	store := NewRouteTable()
	requireOwnerScopedUnpublish(t, store)
}

func TestRouteTableDeleteClearsAllOwners(t *testing.T) {
	store := NewRouteTable()
	requireDeleteClearsOwners(t, store)
}

func TestRedisUnpublishLeavesOtherOwners(t *testing.T) {
	store, _ := newRedisStore(t)
	requireOwnerScopedUnpublish(t, store)
}

func TestRedisDeleteClearsAllOwners(t *testing.T) {
	store, _ := newRedisStore(t)
	requireDeleteClearsOwners(t, store)
}

func TestRedisCandidatesPruneExpiredOwners(t *testing.T) {
	store, client := newRedisStore(t)
	ctx := t.Context()
	tunnelID := "tunnel-routes-expire"
	now := time.Now()

	require.NoError(t, client.ZAdd(ctx, RouteKey(tunnelID),
		redis.Z{Score: float64(now.Add(-time.Minute).UnixMilli()), Member: "expired"},
		redis.Z{Score: float64(now.Add(time.Minute).UnixMilli()), Member: "live"},
	).Err())

	candidates, err := store.Candidates(ctx, tunnelID)
	require.NoError(t, err)
	require.Equal(t, []string{"live"}, candidates)

	err = client.ZScore(ctx, RouteKey(tunnelID), "expired").Err()
	require.ErrorIs(t, err, redis.Nil)
}

func TestRedisConnectionsMergeOwnerSnapshots(t *testing.T) {
	store, _ := newRedisStore(t)
	ctx := t.Context()

	require.NoError(t, store.PublishConnections(ctx, "tunnel-connections", "owner-a", []Connection{
		{GatewaySessionID: "session-a", Metadata: map[string]string{}},
	}, time.Hour))
	require.NoError(t, store.PublishConnections(ctx, "tunnel-connections", "owner-b", []Connection{
		{GatewaySessionID: "session-b", Metadata: map[string]string{}},
	}, time.Hour))

	connections, err := store.Connections(ctx, "tunnel-connections")
	require.NoError(t, err)
	require.Equal(t, []string{"session-a", "session-b"}, connectionSessionIDs(connections))
}

func TestRedisDeleteConnectionOwnerLeavesOtherOwners(t *testing.T) {
	store, _ := newRedisStore(t)
	ctx := t.Context()

	require.NoError(t, store.PublishConnections(ctx, "tunnel-connections-delete-owner", "owner-a", []Connection{
		{GatewaySessionID: "session-a", Metadata: map[string]string{}},
	}, time.Hour))
	require.NoError(t, store.PublishConnections(ctx, "tunnel-connections-delete-owner", "owner-b", []Connection{
		{GatewaySessionID: "session-b", Metadata: map[string]string{}},
	}, time.Hour))

	require.NoError(t, store.DeleteConnectionOwner(ctx, "tunnel-connections-delete-owner", "owner-a"))
	connections, err := store.Connections(ctx, "tunnel-connections-delete-owner")
	require.NoError(t, err)
	require.Equal(t, []string{"session-b"}, connectionSessionIDs(connections))
}

func TestRedisConnectionsFilterExpiredOwners(t *testing.T) {
	store, client := newRedisStore(t)
	ctx := t.Context()
	tunnelID := "tunnel-connections-expire"

	live, err := json.Marshal(connectionSnapshot{
		ExpiresAt: time.Now().Add(time.Hour),
		Connections: []Connection{
			{GatewaySessionID: "live", Metadata: map[string]string{}},
		},
	})
	require.NoError(t, err)
	expired, err := json.Marshal(connectionSnapshot{
		ExpiresAt: time.Now().Add(-time.Hour),
		Connections: []Connection{
			{GatewaySessionID: "expired", Metadata: map[string]string{}},
		},
	})
	require.NoError(t, err)

	require.NoError(t, client.HSet(ctx, ConnectionKey(tunnelID), "live-owner", live, "expired-owner", expired).Err())

	connections, err := store.Connections(ctx, tunnelID)
	require.NoError(t, err)
	require.Equal(t, []string{"live"}, connectionSessionIDs(connections))

	exists, err := client.HExists(ctx, ConnectionKey(tunnelID), "expired-owner").Result()
	require.NoError(t, err)
	require.False(t, exists)
}

func requireOwnerScopedUnpublish(t *testing.T, store Store) {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, store.Publish(ctx, "tunnel-routes", "owner-a", time.Hour))
	require.NoError(t, store.Publish(ctx, "tunnel-routes", "owner-b", time.Hour))

	require.NoError(t, store.Unpublish(ctx, "tunnel-routes", "owner-a"))
	candidates, err := store.Candidates(ctx, "tunnel-routes")
	require.NoError(t, err)
	require.Equal(t, []string{"owner-b"}, candidates)
}

func requireDeleteClearsOwners(t *testing.T, store Store) {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, store.Publish(ctx, "tunnel-routes-delete", "owner-a", time.Hour))
	require.NoError(t, store.Publish(ctx, "tunnel-routes-delete", "owner-b", time.Hour))

	require.NoError(t, store.Delete(ctx, "tunnel-routes-delete"))
	candidates, err := store.Candidates(ctx, "tunnel-routes-delete")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func newRedisStore(t *testing.T) (*Redis, *redis.Client) {
	t.Helper()

	server, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(server.Close)

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		err := client.Close()
		if err != nil && !errors.Is(err, redis.ErrClosed) {
			require.NoError(t, err)
		}
	})
	return NewRedis(client), client
}

func connectionSessionIDs(connections []Connection) []string {
	ids := make([]string, 0, len(connections))
	for _, connection := range connections {
		ids = append(ids, connection.GatewaySessionID)
	}
	sort.Strings(ids)
	return ids
}
