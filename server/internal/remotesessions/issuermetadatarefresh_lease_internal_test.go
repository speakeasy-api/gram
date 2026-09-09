package remotesessions

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestIssuerMetadataRefresh_LeaseLateReleasePreservesNewOwner(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	logger := testenv.NewLogger(t)
	refresher := NewIssuerMetadataRefresher(logger, testenv.NewMeterProvider(t), nil, nil, nil, cache.NewRedisCacheAdapter(client))
	key := issuerMetadataRefreshLockPrefix + "lease-test"
	issuerURL := "https://issuer.example.com"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	require.True(t, refresher.acquire(ctx, logger, key, "owner-a", issuerURL))
	mr.FastForward(issuerMetadataRefreshLockTTL)
	require.True(t, refresher.acquire(t.Context(), logger, key, "owner-b", issuerURL))

	// A resumes after its budget and lease expired. Its detached cleanup must
	// not delete B's newer lease, even though A's context is now cancelled.
	cancel()
	refresher.release(ctx, logger, key, "owner-a")
	owner, err := mr.Get(key)
	require.NoError(t, err)
	require.Equal(t, "owner-b", owner)
	require.False(t, refresher.acquire(t.Context(), logger, key, "owner-c", issuerURL))

	refresher.release(t.Context(), logger, key, "owner-b")
	require.True(t, refresher.acquire(t.Context(), logger, key, "owner-c", issuerURL))
	refresher.release(t.Context(), logger, key, "owner-c")
	require.False(t, mr.Exists(key))
}
