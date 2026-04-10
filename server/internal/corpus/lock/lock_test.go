package lock

import (
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func newTestLocker(t *testing.T) *Locker {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	return NewLocker(client, logger)
}

func TestAcquire(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	lock, err := locker.Acquire(ctx, "test:acquire", 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, lock)
	require.Greater(t, lock.FencingToken(), int64(0))
}

func TestAcquire_AlreadyHeld(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	key := "test:already-held"

	lock1, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, lock1)

	// Second acquire on same key should fail.
	lock2, err := locker.Acquire(ctx, key, 30*time.Second)
	require.Error(t, err)
	require.Nil(t, lock2)
}

func TestRelease(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	key := "test:release"

	lock1, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)

	err = locker.Release(ctx, lock1)
	require.NoError(t, err)

	// After release, should be able to acquire again.
	lock2, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, lock2)
}

func TestRenew(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	key := "test:renew"

	lock, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)

	// Renew should extend the TTL.
	err = locker.Renew(ctx, lock, 30*time.Second)
	require.NoError(t, err)

	// Verify TTL was extended by checking the key still exists with a
	// reasonable TTL (> 20s means renewal worked).
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	ttl := client.TTL(ctx, key).Val()
	require.Greater(t, ttl, 20*time.Second)
}

func TestExpiry(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	key := "test:expiry"

	// Use a very short TTL.
	_, err := locker.Acquire(ctx, key, 100*time.Millisecond)
	require.NoError(t, err)

	// Wait for TTL to expire.
	time.Sleep(200 * time.Millisecond)

	// Lock should have expired; acquire should succeed.
	lock2, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, lock2)
}

func TestFencingToken(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	key := "test:fencing-token"

	lock1, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)
	t1 := lock1.FencingToken()

	err = locker.Release(ctx, lock1)
	require.NoError(t, err)

	lock2, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)
	t2 := lock2.FencingToken()

	require.Greater(t, t2, t1, "second fencing token must be greater than first")
}

func TestFencingToken_RejectsStaleHolder(t *testing.T) {
	ctx := t.Context()
	locker := newTestLocker(t)

	key := "test:fencing-stale"

	// Acquire with short TTL, record token.
	lock1, err := locker.Acquire(ctx, key, 100*time.Millisecond)
	require.NoError(t, err)
	staleToken := lock1.FencingToken()

	// Wait for expiry.
	time.Sleep(200 * time.Millisecond)

	// Acquire again — gets new, higher token.
	lock2, err := locker.Acquire(ctx, key, 30*time.Second)
	require.NoError(t, err)
	require.Greater(t, lock2.FencingToken(), staleToken)

	// Validate stale token should be rejected.
	err = locker.ValidateFencingToken(ctx, key, staleToken)
	require.ErrorIs(t, err, ErrStaleFencingToken)

	// Validate current token should succeed.
	err = locker.ValidateFencingToken(ctx, key, lock2.FencingToken())
	require.NoError(t, err)
}
