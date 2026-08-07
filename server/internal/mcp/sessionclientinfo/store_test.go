package sessionclientinfo

import (
	"testing"

	"github.com/google/uuid"
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
