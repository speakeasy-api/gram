package sessionclientinfo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T, recordCap int) *Store {
	t.Helper()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return NewStore(client, recordCap)
}

func TestStoreLoadRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()

	require.NoError(t, store.Store(t.Context(), projectID, "widgets", "session-1", Info{
		Name:    "claude-code",
		Version: "2.1",
	}, 1))

	got, err := store.Load(t.Context(), projectID, "widgets", "session-1", 2)
	require.NoError(t, err)
	require.Equal(t, Info{Name: "claude-code", Version: "2.1"}, got)
}

func TestLoadUnknownSessionIsNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)

	_, err := store.Load(t.Context(), uuid.New(), "widgets", "never-seen", 1)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestStoreLoadRoundTripsProtocolVersion(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()

	require.NoError(t, store.Store(t.Context(), projectID, "widgets", "session-1", Info{
		Name:            "claude-code",
		Version:         "2.1",
		ProtocolVersion: "2025-06-18",
	}, 1))

	got, err := store.Load(t.Context(), projectID, "widgets", "session-1", 2)
	require.NoError(t, err)
	require.Equal(t, "2025-06-18", got.ProtocolVersion)
}

// TestLoadReturnsRecordCarryingOnlyAProtocolVersion covers a client that omits
// clientInfo.name: the record still attributes its session to a protocol
// generation, so it must survive the round trip rather than reporting absent.
func TestLoadReturnsRecordCarryingOnlyAProtocolVersion(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()

	require.NoError(t, store.Store(t.Context(), projectID, "widgets", "session-1", Info{
		Name:            "",
		Version:         "",
		ProtocolVersion: "2025-06-18",
	}, 1))

	got, err := store.Load(t.Context(), projectID, "widgets", "session-1", 2)
	require.NoError(t, err)
	require.Empty(t, got.Name)
	require.Equal(t, "2025-06-18", got.ProtocolVersion)
}

// TestLoadTreatsAnEmptyRecordAsAbsent pins the other side of that boundary: a
// record with neither a name nor a protocol version tells a caller nothing a
// missing record would not.
func TestLoadTreatsAnEmptyRecordAsAbsent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()

	require.NoError(t, store.Store(t.Context(), projectID, "widgets", "session-1", Info{
		Name:            "",
		Version:         "",
		ProtocolVersion: "",
	}, 1))

	_, err := store.Load(t.Context(), projectID, "widgets", "session-1", 2)
	require.ErrorIs(t, err, ErrNotFound)
}

// TestRecordsAreScopedPerMCPServer covers the tenancy boundary: session ids
// arrive on a client-supplied header, so a record must not be reachable from
// another project or toolset.
func TestRecordsAreScopedPerMCPServer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectA, projectB := uuid.New(), uuid.New()

	require.NoError(t, store.Store(t.Context(), projectA, "widgets", "shared", Info{
		Name:    "claude-code",
		Version: "2.1",
	}, 1))

	_, err := store.Load(t.Context(), projectB, "widgets", "shared", 2)
	require.ErrorIs(t, err, ErrNotFound, "another project must not resolve this session")

	_, err = store.Load(t.Context(), projectA, "gadgets", "shared", 2)
	require.ErrorIs(t, err, ErrNotFound, "another toolset must not resolve this session")
}

// TestCapEvictsLeastRecentlyUsed is the property the whole design rests on:
// initialize is unauthenticated on a public MCP server, so the budget is what
// bounds the damage, and eviction must spare sessions that are still in use no
// matter how old they are.
func TestCapEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 2)
	projectID := uuid.New()
	ctx := t.Context()

	require.NoError(t, store.Store(ctx, projectID, "widgets", "old-but-active", Info{Name: "active", Version: ""}, 1))
	require.NoError(t, store.Store(ctx, projectID, "widgets", "dormant", Info{Name: "dormant", Version: ""}, 2))

	// Using the older record makes it the most recently used, so the dormant
	// one becomes the eviction candidate.
	_, err := store.Load(ctx, projectID, "widgets", "old-but-active", 3)
	require.NoError(t, err)

	require.NoError(t, store.Store(ctx, projectID, "widgets", "newcomer", Info{Name: "newcomer", Version: ""}, 4))

	got, err := store.Load(ctx, projectID, "widgets", "old-but-active", 5)
	require.NoError(t, err, "a session still in use must survive eviction regardless of age")
	require.Equal(t, "active", got.Name)

	_, err = store.Load(ctx, projectID, "widgets", "dormant", 5)
	require.ErrorIs(t, err, ErrNotFound, "the least recently used record is the one that falls out")
}

func TestCapBoundsRecordsPerMCPServer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 3)
	projectID := uuid.New()
	ctx := t.Context()

	for i := range 25 {
		require.NoError(t, store.Store(ctx, projectID, "widgets", uuid.NewString(), Info{
			Name:    "spammer",
			Version: "",
		}, int64(i+1)))
	}

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	live, err := client.ZCard(ctx, liveSetKey(projectID, "widgets")).Result()
	require.NoError(t, err)
	require.EqualValues(t, 3, live, "the live set stays at the cap")

	records, err := client.Keys(ctx, recordKeyPrefix(projectID, "widgets")+"*").Result()
	require.NoError(t, err)
	require.Len(t, records, 3, "evicted records are deleted, not just unlinked from the live set")
}

// idleExpiryFloor is the smallest remaining TTL that still counts as freshly
// set to recordIdleExpiry. A minute absorbs the time the test itself takes
// while staying far above any TTL a test sets by hand.
const idleExpiryFloor = recordIdleExpiry - time.Minute

func requireFreshIdleExpiry(t *testing.T, client *redis.Client, key string) {
	t.Helper()

	ttl, err := client.PTTL(t.Context(), key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, idleExpiryFloor, "key %s must carry a fresh idle expiry", key)
	require.LessOrEqual(t, ttl, recordIdleExpiry, "key %s must not outlive the idle expiry", key)
}

func TestStoreSetsIdleExpiryOnRecordAndLiveSet(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()
	ctx := t.Context()

	require.NoError(t, store.Store(ctx, projectID, "widgets", "session-1", Info{Name: "claude-code", Version: "2.1"}, 1))

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	requireFreshIdleExpiry(t, client, recordKeyPrefix(projectID, "widgets")+member("session-1"))
	requireFreshIdleExpiry(t, client, liveSetKey(projectID, "widgets"))
}

// TestLoadRefreshesIdleExpiry covers the sliding half of the expiry: a session
// that is read keeps its identity, however long ago it was written.
func TestLoadRefreshesIdleExpiry(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()
	ctx := t.Context()
	recordKey := recordKeyPrefix(projectID, "widgets") + member("session-1")
	liveKey := liveSetKey(projectID, "widgets")

	require.NoError(t, store.Store(ctx, projectID, "widgets", "session-1", Info{Name: "claude-code", Version: "2.1"}, 1))

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	// Stand in for most of the expiry having elapsed since the write.
	require.NoError(t, client.PExpire(ctx, recordKey, time.Minute).Err())
	require.NoError(t, client.PExpire(ctx, liveKey, time.Minute).Err())

	_, err = store.Load(ctx, projectID, "widgets", "session-1", 2)
	require.NoError(t, err)

	requireFreshIdleExpiry(t, client, recordKey)
	requireFreshIdleExpiry(t, client, liveKey)
}

// TestIdleRecordExpiresAsNotFound covers the other half: a session idle past
// the expiry reports its caller as unknown, and its live-set member is dropped
// so the budget accounting converges.
func TestIdleRecordExpiresAsNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()
	ctx := t.Context()
	recordKey := recordKeyPrefix(projectID, "widgets") + member("session-1")

	require.NoError(t, store.Store(ctx, projectID, "widgets", "session-1", Info{Name: "claude-code", Version: "2.1"}, 1))

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	// Stand in for the session going idle past its expiry.
	// Wait on the key itself rather than polling Load: a load that lands before
	// the expiry slides it out again, which is the behaviour under test above.
	require.NoError(t, client.PExpire(ctx, recordKey, time.Millisecond).Err())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		exists, err := client.Exists(ctx, recordKey).Result()
		assert.NoError(c, err)
		assert.Zero(c, exists)
	}, 5*time.Second, 10*time.Millisecond)

	_, err = store.Load(ctx, projectID, "widgets", "session-1", 2)
	require.ErrorIs(t, err, ErrNotFound)

	live, err := client.ZCard(ctx, liveSetKey(projectID, "widgets")).Result()
	require.NoError(t, err)
	require.Zero(t, live, "an expired record's live-set member is removed on the next load")
}

// TestLiveSetExpiryDoesNotBreakAdmitOrLoad covers a server whose live set has
// expired: later sessions are admitted and tracked again, and a record that
// outlived its set still loads.
func TestLiveSetExpiryDoesNotBreakAdmitOrLoad(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()
	ctx := t.Context()
	liveKey := liveSetKey(projectID, "widgets")

	require.NoError(t, store.Store(ctx, projectID, "widgets", "session-1", Info{Name: "first", Version: ""}, 1))

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	require.NoError(t, client.PExpire(ctx, liveKey, time.Millisecond).Err())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		exists, err := client.Exists(ctx, liveKey).Result()
		assert.NoError(c, err)
		assert.Zero(c, exists)
	}, 5*time.Second, 10*time.Millisecond)

	got, err := store.Load(ctx, projectID, "widgets", "session-1", 2)
	require.NoError(t, err)
	require.Equal(t, "first", got.Name)

	require.NoError(t, store.Store(ctx, projectID, "widgets", "session-2", Info{Name: "second", Version: ""}, 3))

	got, err = store.Load(ctx, projectID, "widgets", "session-2", 4)
	require.NoError(t, err)
	require.Equal(t, "second", got.Name)

	requireFreshIdleExpiry(t, client, liveKey)
	requireFreshIdleExpiry(t, client, recordKeyPrefix(projectID, "widgets")+member("session-2"))
}

// TestLoadReadmitsRecordAfterLiveSetLoss covers a live set that expired or was
// evicted while a record survived: a hit puts the record back in the set with a
// fresh expiry, so it counts against the cap again.
func TestLoadReadmitsRecordAfterLiveSetLoss(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 0)
	projectID := uuid.New()
	ctx := t.Context()
	recordKey := recordKeyPrefix(projectID, "widgets") + member("session-1")
	liveKey := liveSetKey(projectID, "widgets")

	require.NoError(t, store.Store(ctx, projectID, "widgets", "session-1", Info{Name: "claude-code", Version: "2.1"}, 1))

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	require.NoError(t, client.Del(ctx, liveKey).Err())

	_, err = store.Load(ctx, projectID, "widgets", "session-1", 2)
	require.NoError(t, err)

	score, err := client.ZScore(ctx, liveKey, member("session-1")).Result()
	require.NoError(t, err, "the hit must re-admit the record to the live set")
	require.EqualValues(t, 2, int64(score), "the re-admitted member carries the load's timestamp")

	requireFreshIdleExpiry(t, client, recordKey)
	requireFreshIdleExpiry(t, client, liveKey)
}

// TestCapEvictsRecordsReadmittedByLoad checks how re-admission on a hit meets
// the budget: a load may push the live set over the cap, and the overflow lasts
// only until the next admit evicts the excess, least recently used first.
func TestCapEvictsRecordsReadmittedByLoad(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 1)
	projectID := uuid.New()
	ctx := t.Context()
	liveKey := liveSetKey(projectID, "widgets")

	require.NoError(t, store.Store(ctx, projectID, "widgets", "orphaned", Info{Name: "orphaned", Version: ""}, 1))

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	require.NoError(t, client.Del(ctx, liveKey).Err())
	require.NoError(t, store.Store(ctx, projectID, "widgets", "tracked", Info{Name: "tracked", Version: ""}, 2))

	_, err = store.Load(ctx, projectID, "widgets", "orphaned", 3)
	require.NoError(t, err)

	live, err := client.ZCard(ctx, liveKey).Result()
	require.NoError(t, err)
	require.EqualValues(t, 2, live, "the hit re-admits the orphaned record above the cap")

	require.NoError(t, store.Store(ctx, projectID, "widgets", "newcomer", Info{Name: "newcomer", Version: ""}, 4))

	live, err = client.ZCard(ctx, liveKey).Result()
	require.NoError(t, err)
	require.EqualValues(t, 1, live, "the next admit brings the live set back to the cap")

	records, err := client.Keys(ctx, recordKeyPrefix(projectID, "widgets")+"*").Result()
	require.NoError(t, err)
	require.Len(t, records, 1, "evicted records are deleted")

	got, err := store.Load(ctx, projectID, "widgets", "newcomer", 5)
	require.NoError(t, err)
	require.Equal(t, "newcomer", got.Name)
}

// TestStoreWithoutRedisIsInert keeps environments with no cache wired working:
// every caller simply resolves as unknown rather than erroring.
func TestStoreWithoutRedisIsInert(t *testing.T) {
	t.Parallel()

	store := NewStore(nil, 0)

	require.NoError(t, store.Store(t.Context(), uuid.New(), "widgets", "session-1", Info{
		Name:    "claude-code",
		Version: "2.1",
	}, 1))

	_, err := store.Load(t.Context(), uuid.New(), "widgets", "session-1", 2)
	require.ErrorIs(t, err, ErrNotFound)
}
