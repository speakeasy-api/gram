package route

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

var errStoreDown = errors.New("store down")

// downStore fails every command.
type downStore struct{}

func (downStore) Publish(context.Context, string, string, time.Duration) error { return errStoreDown }
func (downStore) Candidates(context.Context, string) ([]string, error)         { return nil, errStoreDown }
func (downStore) Unpublish(context.Context, string, string) error              { return errStoreDown }
func (downStore) Delete(context.Context, string) error                         { return errStoreDown }

func newAuthMirrorStore(t *testing.T) (Store, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewAuthMirror(client), server
}

func TestAuthMirrorUsesItsOwnKey(t *testing.T) {
	t.Parallel()

	mirror, server := newAuthMirrorStore(t)
	require.NoError(t, mirror.Publish(t.Context(), "tunnel-1", "gateway-a", 30*time.Second))

	require.True(t, server.Exists(AuthRouteKey("tunnel-1")))
	require.False(t, server.Exists(RouteKey("tunnel-1")))
	require.Equal(t, 30*time.Second, server.TTL(AuthRouteKey("tunnel-1")))
}

func TestAuthMirrorUnpublishLeavesOtherOwners(t *testing.T) {
	t.Parallel()

	mirror, _ := newAuthMirrorStore(t)
	requireOwnerScopedUnpublish(t, mirror)
}

func TestAuthMirrorDeleteClearsAllOwners(t *testing.T) {
	t.Parallel()

	mirror, _ := newAuthMirrorStore(t)
	requireDeleteClearsOwners(t, mirror)
}

func TestAuthMirrorRouteExpires(t *testing.T) {
	t.Parallel()

	mirror, server := newAuthMirrorStore(t)
	require.NoError(t, mirror.Publish(t.Context(), "tunnel-1", "gateway-a", 30*time.Second))
	server.FastForward(31 * time.Second)

	candidates, err := mirror.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestMirrorReaderServesMirroredTunnelWithoutBulkStore(t *testing.T) {
	t.Parallel()

	mirror := NewRouteTable()
	require.NoError(t, mirror.Publish(t.Context(), "tunnel-1", "gateway-a", time.Minute))
	reader := NewMirrorReader(mirror, downStore{})

	candidates, err := reader.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"gateway-a"}, candidates)
}

func TestMirrorReaderFallsBackToBulkStoreForUnmirroredTunnel(t *testing.T) {
	t.Parallel()

	bulk := NewRouteTable()
	require.NoError(t, bulk.Publish(t.Context(), "tunnel-1", "gateway-b", time.Minute))
	reader := NewMirrorReader(NewRouteTable(), bulk)

	candidates, err := reader.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"gateway-b"}, candidates)
}

func TestMirrorReaderFallsBackToBulkStoreWhenMirrorFails(t *testing.T) {
	t.Parallel()

	bulk := NewRouteTable()
	require.NoError(t, bulk.Publish(t.Context(), "tunnel-1", "gateway-b", time.Minute))
	reader := NewMirrorReader(downStore{}, bulk)

	candidates, err := reader.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"gateway-b"}, candidates)
}

func TestMirrorReaderReportsBothFailures(t *testing.T) {
	t.Parallel()

	reader := NewMirrorReader(downStore{}, downStore{})

	_, err := reader.Candidates(t.Context(), "tunnel-1")
	require.ErrorIs(t, err, errStoreDown)
}

func TestMirrorReaderUnpublishRemovesFromBothStores(t *testing.T) {
	t.Parallel()

	mirror := NewRouteTable()
	bulk := NewRouteTable()
	for _, store := range []Store{mirror, bulk} {
		require.NoError(t, store.Publish(t.Context(), "tunnel-1", "gateway-a", time.Minute))
	}
	reader := NewMirrorReader(mirror, bulk)

	require.NoError(t, reader.Unpublish(t.Context(), "tunnel-1", "gateway-a"))
	for _, store := range []Store{mirror, bulk} {
		candidates, err := store.Candidates(t.Context(), "tunnel-1")
		require.NoError(t, err)
		require.Empty(t, candidates)
	}
}

func TestMirrorReaderUnpublishClearsMirrorWhenBulkStoreIsDown(t *testing.T) {
	t.Parallel()

	mirror := NewRouteTable()
	require.NoError(t, mirror.Publish(t.Context(), "tunnel-1", "gateway-a", time.Minute))
	reader := NewMirrorReader(mirror, downStore{})

	require.ErrorIs(t, reader.Unpublish(t.Context(), "tunnel-1", "gateway-a"), errStoreDown)
	candidates, err := mirror.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, candidates)
}
