package channels

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
)

// TestConcurrentWritesNeverSkipACursor is the cursor-correctness gate.
//
// Allocating a sequence before commit lets a transaction that took a LOWER
// number commit AFTER a higher one became visible. A reader paging on
// `seq > cursor` would move its cursor past the gap and lose that message
// forever. The sequence is therefore allocated at the end of the writing
// transaction, under a row lock held until commit, so commit order and
// sequence order agree.
//
// The reader below runs concurrently with the writers and only ever moves
// forward, exactly as a real client does, so it fails if any message is
// skipped rather than merely delivered late.
func TestConcurrentWritesNeverSkipACursor(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_cursor_order")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Concurrent",
	})

	const writers = 32
	ctx := t.Context()
	// The poller only collects; every assertion happens on the test goroutine
	// once it has finished.
	seen := map[int64]bool{}
	var pollErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		cursor := int64(0)
		deadline := time.After(60 * time.Second)
		for {
			page, status, err := f.request(ctx, "alice", "channels_read", map[string]any{"cursor": cursor, "limit": 10})
			if err != nil || status != http.StatusOK {
				pollErr = fmt.Errorf("read failed with status %d: %v %w", status, page, err)
				return
			}
			messages, ok := page["messages"].([]any)
			if !ok {
				pollErr = fmt.Errorf("read returned no message list: %v", page)
				return
			}
			for _, item := range messages {
				message, ok := item.(map[string]any)
				if !ok {
					pollErr = fmt.Errorf("read returned a malformed message: %v", item)
					return
				}
				id, ok := message["id"].(float64)
				if !ok {
					pollErr = fmt.Errorf("read returned a message without an id: %v", message)
					return
				}
				seen[int64(id)] = true
			}
			next, ok := page["cursor"].(float64)
			if !ok {
				pollErr = fmt.Errorf("read returned no cursor: %v", page)
				return
			}
			cursor = int64(next)
			if len(seen) >= writers {
				return
			}
			select {
			case <-deadline:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()

	var writing sync.WaitGroup
	for index := range writers {
		writing.Go(func() {
			f.post(t, "alice", "channels_post", map[string]any{
				"instanceId": alice["instanceId"], "threadId": thread["id"],
				"text": "concurrent", "idempotencyKey": "race-" + strconv.Itoa(index),
			})
		})
	}
	writing.Wait()
	<-done
	require.NoError(t, pollErr)
	require.Len(t, seen, writers, "a forward-only reader lost messages to out-of-order commits")

	// Every stored sequence is distinct, which is what makes the cursor a
	// total order rather than a hint.
	stats, err := repo.New(f.db).FixtureMessageSeqStats(t.Context(), testOrg)
	require.NoError(t, err)
	require.Equal(t, stats.Total, stats.DistinctSeqs)
}

// TestSimultaneousDashboardSessionsConverge proves the browser session upsert
// is atomic: parallel first sends and reloads share one session instead of
// leaving a trail of abandoned ones.
func TestSimultaneousDashboardSessionsConverge(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_session_race")
	f.enable(t, "alice")

	const attempts = 8
	ids := make([]string, attempts)
	var opening sync.WaitGroup
	for index := range attempts {
		opening.Go(func() {
			body, status := f.try(t, "alice", "channels_dashboard_session", map[string]any{})
			if status == http.StatusOK {
				ids[index], _ = body["instanceId"].(string)
			}
		})
	}
	opening.Wait()
	for _, id := range ids {
		require.NotEmpty(t, id)
		require.Equal(t, ids[0], id, "simultaneous requests opened more than one browser session")
	}
	sessions, err := repo.New(f.db).FixtureCountDashboardInstances(t.Context(), repo.FixtureCountDashboardInstancesParams{OrganizationID: testOrg, SubjectID: "alice"})
	require.NoError(t, err)
	require.Equal(t, int64(1), sessions)
}

// TestSimultaneousRetriesReturnOneMessage proves sender-scoped idempotency is
// atomic under contention: a retry storm writes once and every caller receives
// that same message.
func TestSimultaneousRetriesReturnOneMessage(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_idempotency_race")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Retries",
	})

	const attempts = 8
	results := make([]int64, attempts)
	var retrying sync.WaitGroup
	for index := range attempts {
		retrying.Go(func() {
			body, status := f.try(t, "alice", "channels_post", map[string]any{
				"instanceId": alice["instanceId"], "threadId": thread["id"],
				"text": "one logical message", "idempotencyKey": "storm",
			})
			if status == http.StatusOK {
				if id, ok := body["id"].(float64); ok {
					results[index] = int64(id)
				}
			}
		})
	}
	retrying.Wait()
	for _, id := range results {
		require.NotZero(t, id)
		require.Equal(t, results[0], id, "a retry storm produced more than one message")
	}
	stored, err := repo.New(f.db).FixtureCountMessagesByIdempotencyKey(t.Context(), repo.FixtureCountMessagesByIdempotencyKeyParams{OrganizationID: testOrg, IdempotencyKey: "storm"})
	require.NoError(t, err)
	require.Equal(t, int64(1), stored)
}

// TestCrossTenantConcurrencyStaysIsolated runs both organizations at once and
// proves neither sequence space nor content crosses the boundary.
func TestCrossTenantConcurrencyStaysIsolated(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_tenant_race")
	f.enable(t, "alice")
	f.enable(t, "stranger")
	alice := f.connect(t, "alice", "Here")
	stranger := f.connect(t, "stranger", "Elsewhere")
	here := f.post(t, "alice", "channels_create_thread", map[string]any{"instanceId": alice["instanceId"], "title": "Here"})
	elsewhere := f.post(t, "stranger", "channels_create_thread", map[string]any{"instanceId": stranger["instanceId"], "title": "Elsewhere"})

	var writing sync.WaitGroup
	for index := range 12 {
		writing.Go(func() {
			f.post(t, "alice", "channels_post", map[string]any{
				"instanceId": alice["instanceId"], "threadId": here["id"],
				"text": "here", "idempotencyKey": "here-" + strconv.Itoa(index),
			})
		})
		writing.Go(func() {
			f.post(t, "stranger", "channels_post", map[string]any{
				"instanceId": stranger["instanceId"], "threadId": elsewhere["id"],
				"text": "elsewhere", "idempotencyKey": "elsewhere-" + strconv.Itoa(index),
			})
		})
	}
	writing.Wait()

	require.Len(t, messageIDs(t, f.post(t, "alice", "channels_read", map[string]any{"limit": 200})["messages"]), 12)
	require.Len(t, messageIDs(t, f.post(t, "stranger", "channels_read", map[string]any{"limit": 200})["messages"]), 12)
	// Each organization numbers its own messages from one.
	for _, organization := range []string{testOrg, otherOrg} {
		stats, err := repo.New(f.db).FixtureMessageSeqStats(t.Context(), organization)
		require.NoError(t, err)
		require.Equal(t, int64(1), stats.Lowest, "%s did not start its own cursor space", organization)
		require.Equal(t, int64(12), stats.Highest, "%s shared a cursor space", organization)
	}
}

// TestWriteBurstIsBounded proves a runaway writer is refused with a retryable
// outcome rather than a permission error.
func TestWriteBurstIsBounded(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_burst")
	f := newFixtureOn(t, db)
	f.handler.writes = newBurstLimiter(60, 3)
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Burst",
	})
	limited := false
	for index := range 12 {
		body, status := f.try(t, "alice", "channels_post", map[string]any{
			"instanceId": alice["instanceId"], "threadId": thread["id"],
			"text": "burst", "idempotencyKey": "burst-" + strconv.Itoa(index),
		})
		if status == http.StatusTooManyRequests {
			limited = true
			require.Contains(t, body["error"], "retry with the same idempotency key")
			break
		}
	}
	require.True(t, limited, "an unbounded write burst was accepted")
}

// TestConcurrentSubscriptionsAreCapped proves one identity cannot occupy every
// stream slot on a replica.
func TestConcurrentSubscriptionsAreCapped(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_stream_cap")
	f := newFixtureOn(t, db)
	f.handler.limits.StreamsPerSubject = 2
	f.enable(t, "alice")

	for index := range 2 {
		subscription := f.listen(t, "bob", "stream-"+strconv.Itoa(index), "channels://presence")
		require.Equal(t, "notifications/subscriptions/acknowledged", subscription.next(t)["method"])
	}
	refused := f.rpc(t, "bob", "subscriptions/listen", map[string]any{
		"notifications": map[string]any{"resourceSubscriptions": []string{"channels://presence"}},
	})
	require.NotNil(t, refused["error"], "the per-identity subscription cap was not enforced")
}
