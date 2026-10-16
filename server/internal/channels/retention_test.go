package channels

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// TestRetentionRemovesHistoryAndReportsStaleCursors proves retention is
// bounded, tenant-isolated, and honest with clients: a cursor that fell below
// the retention floor is told so instead of silently receiving a short page.
func TestRetentionRemovesHistoryAndReportsStaleCursors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_retention")
	f.enable(t, "alice")
	f.enable(t, "stranger")
	alice := f.connect(t, "alice", "Writer")
	stranger := f.connect(t, "stranger", "Elsewhere")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Aging",
	})
	elsewhere := f.post(t, "stranger", "channels_create_thread", map[string]any{
		"instanceId": stranger["instanceId"], "title": "Elsewhere",
	})
	for index := range 6 {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "aging", "idempotencyKey": "age-" + strconv.Itoa(index),
		})
	}
	f.post(t, "stranger", "channels_post", map[string]any{
		"instanceId": stranger["instanceId"], "threadId": elsewhere["id"],
		"text": "untouched", "idempotencyKey": "elsewhere-1",
	})

	// Age the first four messages past the organization's retention window.
	err := repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: 4})
	require.NoError(t, err)
	err = repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: otherOrg, AgeDays: 200, ThroughSeq: math.MaxInt64})
	require.NoError(t, err)
	// Only this organization keeps a short window; the other keeps everything.
	err = repo.New(f.db).FixtureSetMessageRetentionDays(t.Context(), repo.FixtureSetMessageRetentionDaysParams{OrganizationID: otherOrg, MessageRetentionDays: 1000})
	require.NoError(t, err)

	swept, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 4, swept.Messages)
	require.Positive(t, swept.Organizations)

	// The other organization is untouched: retention is tenant-isolated.
	require.Len(t, messageIDs(t, f.post(t, "stranger", "channels_read", map[string]any{})["messages"]), 1)

	remaining := messageIDs(t, f.post(t, "alice", "channels_read", map[string]any{"cursor": 4})["messages"])
	require.Equal(t, []int64{5, 6}, remaining, "retention removed more than the aged messages")

	// A cursor below the floor is explicitly stale, not quietly short.
	stale := f.post(t, "alice", "channels_read", map[string]any{"cursor": 0})
	require.True(t, boolOf(t, stale["stale"]))
	require.Equal(t, int64(4), intOf(t, stale["cursor"]), "a stale read reports the floor to restart from")
	require.Empty(t, messageIDs(t, stale["messages"]))

	// History below the floor is gone and says so.
	below := f.post(t, "alice", "channels_history", map[string]any{"threadId": thread["id"], "before": 3})
	require.True(t, boolOf(t, below["stale"]))
	require.Empty(t, messageIDs(t, below["messages"]))

	// The newest page is still served, and reaching its bottom reports that
	// retention has removed what lies beyond.
	newest := f.post(t, "alice", "channels_history", map[string]any{"threadId": thread["id"]})
	require.Equal(t, []int64{5, 6}, messageIDs(t, newest["messages"]))
	require.False(t, boolOf(t, newest["hasMore"]))
	require.True(t, boolOf(t, newest["stale"]))
}

// TestRetentionKeepsAttribution proves an instance that still names an author
// survives retirement, so history never loses whose message it was.
func TestRetentionKeepsAttribution(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_retention_attribution")
	f.enable(t, "alice")
	speaker := f.connect(t, "alice", "Speaker")
	silent := f.connect(t, "alice", "Silent")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": speaker["instanceId"], "title": "Attribution",
	})
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": speaker["instanceId"], "threadId": thread["id"],
		"text": "spoken", "idempotencyKey": "spoken-1",
	})
	err := repo.New(f.db).FixtureAgeInstances(t.Context(), repo.FixtureAgeInstancesParams{OrganizationID: testOrg, AgeDays: 200})
	require.NoError(t, err)

	swept, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, swept.Instances, "only the instance with nothing to attribute was retired")

	surviving, err := repo.New(f.db).ListInstancesByIDs(t.Context(), repo.ListInstancesByIDsParams{
		OrganizationID: testOrg,
		Ids:            []uuid.UUID{mustUUID(t, silent["instanceId"]), mustUUID(t, speaker["instanceId"])},
	})
	require.NoError(t, err)
	require.Len(t, surviving, 1, "retirement removed the wrong instances")
	require.Equal(t, mustUUID(t, speaker["instanceId"]), surviving[0].ID, "an instance that still names an author was removed")

	// The expired speaker is absent from presence but still names its message.
	snapshot := f.snapshot(t, "alice")
	require.Empty(t, snapshot["instances"], "an expired lease still appeared as present")
	require.Contains(t, mapOf(t, snapshot["participants"]), speaker["instanceId"], "attribution was lost after the lease expired")
}

// TestRetentionIsBounded proves one pass never exceeds its batch, so a large
// backlog cannot turn into one enormous statement.
func TestRetentionIsBounded(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_retention_bounded")
	f := newFixtureOn(t, db)
	f.handler.retention = Retention{Batch: 3, Organizations: 100}
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Backlog",
	})
	for index := range 10 {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "old", "idempotencyKey": "old-" + strconv.Itoa(index),
		})
	}
	err := repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: math.MaxInt64})
	require.NoError(t, err)

	first, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 3, first.Messages, "a pass exceeded its batch")
	require.True(t, first.Truncated, "a truncated pass did not say so")

	total := first.Messages
	for range 10 {
		pass, err := f.handler.RunRetention(t.Context(), time.Now())
		require.NoError(t, err)
		total += pass.Messages
		if pass.Messages == 0 {
			break
		}
	}
	require.Equal(t, 10, total, "repeated passes did not clear the backlog")
}

// TestRetentionSkipsOrganizationsWithNothingToDo proves the sweep's first
// query returns only organizations that actually have something to retire. A
// deployment where nobody has aged data must not cost two statements per
// organization that ever used Channels.
func TestRetentionSkipsOrganizationsWithNothingToDo(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_retention_idle")
	f.enable(t, "alice")
	f.enable(t, "stranger")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Fresh",
	})
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": thread["id"],
		"text": "fresh", "idempotencyKey": "fresh-1",
	})

	idle, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Zero(t, idle.Organizations, "a sweep visited an organization with nothing to retire")
	require.Zero(t, idle.Messages)
	require.False(t, idle.Truncated)

	// Age one organization's data; only that organization is visited.
	err = repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: math.MaxInt64})
	require.NoError(t, err)
	swept, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, swept.Organizations)
	require.Equal(t, 1, swept.Messages)
}

// TestRetentionAnnouncesItselfToSubscribers proves the sweep publishes the
// same transactional delivery hint a write does. Retention runs in the worker,
// not in the process holding subscriptions, so a process-local wake-up would
// reach nobody — the hint has to travel through PostgreSQL.
func TestRetentionAnnouncesItselfToSubscribers(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_retention_hint")
	writer := newFixtureOn(t, db)
	// A second handler stands in for the server process that holds the
	// subscriptions while the sweep runs somewhere else entirely.
	reader := newFixtureOn(t, db)

	writer.enable(t, "alice")
	alice := writer.connect(t, "alice", "Writer")
	thread := writer.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Aging",
	})

	// Subscribe first, then write until one of the writes' hints reaches this
	// subscriber: the reader's listener connects asynchronously, and a hint
	// published before it listens is gone. Once one arrives the listener is
	// live, so the next wake-up after the drain below is retention's own.
	wake, unsubscribe := reader.handler.events.subscribe(testOrg)
	defer unsubscribe()
	delivered := false
	for attempt := 0; attempt < 20 && !delivered; attempt++ {
		writer.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "aging", "idempotencyKey": fmt.Sprintf("aging-%d", attempt),
		})
		select {
		case <-wake:
			delivered = true
		case <-time.After(time.Second):
		}
	}
	require.True(t, delivered, "the reader never received a write's delivery hint")
	select {
	case <-wake:
	default:
	}

	err := repo.New(writer.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: math.MaxInt64})
	require.NoError(t, err)
	swept, err := writer.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Positive(t, swept.Messages)

	select {
	case <-wake:
	case <-time.After(20 * time.Second):
		t.Fatal("retention did not announce itself to a subscriber on another handler")
	}
}

// TestSnapshotCursorNeverFallsBelowTheRetentionFloor pins the fix for a
// reload loop: a snapshot whose readable tail is empty must still hand out a
// cursor at or above the floor, or the next forward read rejects it as stale
// and a client that reloads on stale bounces between the two forever.
func TestSnapshotCursorNeverFallsBelowTheRetentionFloor(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_retention_cursor")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "All aged out",
	})
	for index := range 3 {
		f.post(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "aged", "idempotencyKey": "aged-" + strconv.Itoa(index),
		})
	}
	err := repo.New(f.db).FixtureAgeMessages(t.Context(), repo.FixtureAgeMessagesParams{OrganizationID: testOrg, AgeDays: 200, ThroughSeq: math.MaxInt64})
	require.NoError(t, err)
	swept, err := f.handler.RunRetention(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 3, swept.Messages)

	snapshot := f.snapshot(t, "alice")
	require.Empty(t, messageIDs(t, snapshot["messages"]), "every message was retired")
	require.Equal(t, int64(3), intOf(t, snapshot["cursor"]), "the snapshot cursor fell below the retention floor")

	// The cursor the snapshot just handed out must not be rejected as stale.
	forward := f.post(t, "alice", "channels_read", map[string]any{"cursor": snapshot["cursor"]})
	require.False(t, boolOf(t, forward["stale"]), "the snapshot handed out a cursor its own read rejects")
	require.False(t, boolOf(t, forward["hasMore"]))
	require.Empty(t, messageIDs(t, forward["messages"]))
}
