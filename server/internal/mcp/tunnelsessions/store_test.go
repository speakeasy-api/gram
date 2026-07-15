package tunnelsessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T, ttl time.Duration, liveCap int) *Store {
	t.Helper()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return NewStore(client, ttl, liveCap)
}

func TestMintSessionIDShapeAndUniqueness(t *testing.T) {
	t.Parallel()

	first, err := MintSessionID()
	require.NoError(t, err)
	second, err := MintSessionID()
	require.NoError(t, err)

	require.True(t, IsSessionID(first))
	require.True(t, IsSessionID(second))
	require.NotEqual(t, first, second)
}

func TestIsSessionIDRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"gsid_",
		"gsid_XYZ",
		"gsid_" + "0123456789abcdef",             // too short
		"gsid_0123456789abcdef0123456789abcdef0", // too long
		"GSID_0123456789abcdef0123456789abcdef",  // wrong case prefix
		"gsid_0123456789ABCDEF0123456789ABCDEF",  // upper hex
		"backend-session-id",                     // arbitrary backend value
		"gsid_0123456789abcdef0123456789abcdef\n",                  // trailing newline
		"tunnelsess:map:x:y:gsid_0123456789abcdef0123456789abcdef", // injection attempt
	} {
		require.False(t, IsSessionID(value), "value %q must be rejected", value)
	}
}

func TestReserveCommitResolveRoundTrip(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 10)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
	session := Session{BackendSessionID: "backend-abc", GatewayAddr: "10.0.0.1:9000", AgentSessionID: "agent-1"}
	require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, session))

	resolved, err := store.Resolve(t.Context(), tunnelID, "server-1", sid, true)
	require.NoError(t, err)
	require.Equal(t, &session, resolved)

	count, err := store.ActiveCount(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestResolveUnknownSessionIsNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 10)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	_, err = store.Resolve(t.Context(), tunnelID, "server-1", sid, true)
	require.ErrorIs(t, err, ErrNotFound)

	_, err = store.Resolve(t.Context(), tunnelID, "server-1", sid, false)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestResolveIsNamespacedByTunnelAndServer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 10)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
	require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"}))

	_, err = store.Resolve(t.Context(), tunnelID+"-other", "server-1", sid, true)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = store.Resolve(t.Context(), tunnelID, "server-2", sid, true)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRollbackReleasesCapacitySlot(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 1)
	tunnelID := t.Name()
	first, err := MintSessionID()
	require.NoError(t, err)
	second, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", first))

	err = store.Reserve(t.Context(), tunnelID, "server-1", second)
	var capErr *CapacityError
	require.ErrorAs(t, err, &capErr)
	require.GreaterOrEqual(t, capErr.RetryAfter, time.Second)

	require.NoError(t, store.Rollback(t.Context(), tunnelID, "server-1", first))
	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", second))
}

func TestReservePrunesExpiredSessions(t *testing.T) {
	t.Parallel()

	// TTL short enough to expire between calls without violating the
	// no-time.Sleep rule: EventuallyWithT polls until the slot frees.
	store := newTestStore(t, 100*time.Millisecond, 1)
	tunnelID := t.Name()
	first, err := MintSessionID()
	require.NoError(t, err)
	second, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", first))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, store.Reserve(t.Context(), tunnelID, "server-1", second))
	}, 5*time.Second, 20*time.Millisecond)
}

func TestDeleteRemovesMappingAndSlot(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 1)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
	require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"}))
	require.NoError(t, store.Delete(t.Context(), tunnelID, "server-1", sid))

	_, err = store.Resolve(t.Context(), tunnelID, "server-1", sid, false)
	require.ErrorIs(t, err, ErrNotFound)

	count, err := store.ActiveCount(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestPurgeDropsAllTunnelSessions(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 10)
	tunnelID := t.Name()
	var sids []string
	for range 3 {
		sid, err := MintSessionID()
		require.NoError(t, err)
		require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
		require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"}))
		sids = append(sids, sid)
	}

	require.NoError(t, store.Purge(t.Context(), tunnelID))

	for _, sid := range sids {
		_, err := store.Resolve(t.Context(), tunnelID, "server-1", sid, false)
		require.ErrorIs(t, err, ErrNotFound)
	}
	count, err := store.ActiveCount(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Zero(t, count)
}

// TestResolveWithoutRefreshDoesNotExtendSession: DELETE-path resolution must
// not slide the session lifetime forward.
func TestResolveWithoutRefreshDoesNotExtendSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 200*time.Millisecond, 10)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
	require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"}))

	// Keep resolving without refresh; the session must still expire on its
	// original TTL rather than being kept alive by the reads.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, resolveErr := store.Resolve(t.Context(), tunnelID, "server-1", sid, false)
		assert.ErrorIs(c, resolveErr, ErrNotFound)
	}, 5*time.Second, 20*time.Millisecond)
}

// TestCommitRequiresLiveReservation: a Commit after the reservation's live-set
// member is gone (a concurrent Purge won) must fail with ErrReservationLost
// and must not recreate the mapping.
func TestCommitRequiresLiveReservation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 10)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
	// Purge (consent withdrawn) removes the reservation before Commit.
	require.NoError(t, store.Purge(t.Context(), tunnelID))

	err = store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"})
	require.ErrorIs(t, err, ErrReservationLost)

	_, err = store.Resolve(t.Context(), tunnelID, "server-1", sid, false)
	require.ErrorIs(t, err, ErrNotFound)
	count, err := store.ActiveCount(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Zero(t, count)
}

// TestCommitRealignsLiveSetToMappingTTL: a delayed Commit must re-align the
// live-set member's expiry to the mapping's fresh TTL, so a mapping cannot
// outlive its live-set member (which would leave it uncounted and
// unpurgeable). A refreshed session must never drift into resolvable-but-
// uncounted.
func TestCommitRealignsLiveSetToMappingTTL(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 400*time.Millisecond, 10)
	tunnelID := t.Name()
	sid, err := MintSessionID()
	require.NoError(t, err)

	require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
	require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"}))

	deadline := time.Now().Add(1200 * time.Millisecond)
	require.Never(t, func() bool {
		if time.Now().After(deadline) {
			return false
		}
		_, resolveErr := store.Resolve(t.Context(), tunnelID, "server-1", sid, true)
		count, countErr := store.ActiveCount(t.Context(), tunnelID)
		// A drift bug shows as: mapping resolvable but count==0.
		return resolveErr == nil && countErr == nil && count == 0
	}, 800*time.Millisecond, 50*time.Millisecond)
}

// TestPurgeIsAtomicAcrossMembers: Purge drops every mapping AND the live set
// in one shot, leaving nothing resolvable or counted.
func TestPurgeIsAtomicAcrossMembers(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, time.Hour, 10)
	tunnelID := t.Name()
	var sids []string
	for range 4 {
		sid, err := MintSessionID()
		require.NoError(t, err)
		require.NoError(t, store.Reserve(t.Context(), tunnelID, "server-1", sid))
		require.NoError(t, store.Commit(t.Context(), tunnelID, "server-1", sid, Session{BackendSessionID: "b", GatewayAddr: "a", AgentSessionID: "s"}))
		sids = append(sids, sid)
	}

	require.NoError(t, store.Purge(t.Context(), tunnelID))

	for _, sid := range sids {
		_, err := store.Resolve(t.Context(), tunnelID, "server-1", sid, false)
		require.ErrorIs(t, err, ErrNotFound)
	}
	count, err := store.ActiveCount(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Zero(t, count)
}
