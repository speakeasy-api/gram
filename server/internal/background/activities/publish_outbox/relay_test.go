package publish_outbox_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/topics"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestDrain_EmptyOutbox(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, result.Published)
	require.False(t, result.HasMore)
	require.Empty(t, inst.pub.messages())
}

func TestDrain_DeletesPublishedRows(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	row := seedRow(t, inst.conn, orgID, seedOptions{})

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Published)
	require.Equal(t, 0, result.DeadLettered)

	require.Equal(t, int64(0), countRows(t, inst.conn), "published row should be deleted, not marked")

	published := inst.pub.messages()
	require.Len(t, published, 1)
	require.Equal(t, webhooksTopic, published[0].Topic)
	require.Equal(t, "audit_log.asset_event_v1", published[0].Attributes["event_type"],
		"stored attributes must reach the publisher so subscription filters and trace context survive the database hop")

	_, err = testrepo.New(inst.conn).GetPublishOutboxDeadLetter(t.Context(), row.PublicID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestDrain_TransientFailureSchedulesRetry(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	row := seedRow(t, inst.conn, orgID, seedOptions{})

	inst.pub.failWith = func(string, int) error {
		return errors.New("pubsub unavailable")
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, result.Published)
	require.Equal(t, 0, result.DeadLettered)
	require.Equal(t, 1, result.Retrying)

	stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), stored.Attempts, "claiming counts as an attempt")
	require.True(t, stored.RetryAfter.Valid, "a transient failure must schedule a retry")
	require.False(t, stored.LockedUntil.Valid, "the lease must be released so the row is claimable again")
	require.Contains(t, stored.LastError.String, "pubsub unavailable")

	require.Empty(t, inst.pub.messages(),
		"a row kept for retry must not also have been delivered, or the retry duplicates it")
}

// TestDrain_RetryingRowsRecordEachRowsOwnError is the dead letter requirement
// applied to rows that are still pending. `last_error` is what anyone looking
// into a stuck row reads, and the relay names the topic it could not reach in
// it — so one shared string labels most of the batch with a topic they have
// nothing to do with, even when a single outage is what stopped all of them.
func TestDrain_RetryingRowsRecordEachRowsOwnError(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	rows := []testrepo.SeedPublishOutboxRowRow{
		seedRow(t, inst.conn, orgID, seedOptions{topic: "gram.alpha.v1.Event"}),
		seedRow(t, inst.conn, orgID, seedOptions{topic: "gram.beta.v1.Event"}),
	}

	inst.pub.failWith = func(string, int) error {
		return errors.New("pubsub unavailable")
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, result.Retrying)

	for _, row := range rows {
		stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), row.ID)
		require.NoError(t, err)
		require.Contains(t, stored.LastError.String, stored.Topic,
			"a retrying row must record why it failed, not why the last row in the batch did")
	}
}

// TestDrain_RetryBackoffFollowsEachRowsOwnAttemptCount pins the back-off to the
// row rather than to the batch. Claims are ordered by id, so a batch routinely
// mixes rows that have been failing for a while with rows enqueued seconds ago.
// Settling all of them on the youngest row's attempt count collapses the
// exponential schedule for the older ones, and while traffic keeps arriving
// there is always a young row — so back-off never escalates and a batch burns
// its whole attempt budget within a minute of the first failure.
func TestDrain_RetryBackoffFollowsEachRowsOwnAttemptCount(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	// Seeded first so it holds the lowest id: this is the row whose attempt
	// count used to dictate the delay for everything claimed alongside it.
	fresh := seedRow(t, inst.conn, orgID, seedOptions{})

	// Claiming bumps these to 9 — one short of the dead-letter threshold, and
	// far enough along the 5s * 2^attempts curve that it has saturated at the
	// ten minute cap.
	const agedCount = 20
	aged := make([]testrepo.SeedPublishOutboxRowRow, 0, agedCount)
	for range agedCount {
		aged = append(aged, seedRow(t, inst.conn, orgID, seedOptions{attempts: 8}))
	}

	inst.pub.failWith = func(string, int) error {
		return errors.New("pubsub unavailable")
	}

	before := time.Now()
	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, agedCount+1, result.Retrying)
	require.Equal(t, 0, result.DeadLettered)

	// A row on attempt 1 draws its delay from the next ten seconds. The extra
	// margin covers the drain itself, which runs after this ceiling's origin.
	freshCeiling := before.Add(15 * time.Second)

	stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), fresh.ID)
	require.NoError(t, err)
	require.True(t, stored.RetryAfter.Valid)
	require.WithinRange(t, stored.RetryAfter.Time, before, freshCeiling,
		"a first attempt must not inherit the longer back-off of the rows it was claimed with")

	windows := make(map[int64]struct{}, agedCount)
	beyondFreshCeiling := 0
	for _, row := range aged {
		storedAged, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), row.ID)
		require.NoError(t, err)
		require.True(t, storedAged.RetryAfter.Valid)

		windows[storedAged.RetryAfter.Time.UnixNano()] = struct{}{}
		if storedAged.RetryAfter.Time.After(freshCeiling) {
			beyondFreshCeiling++
		}
	}

	// Each aged row draws from a ten minute window, so the odds of all twenty
	// landing inside the fresh row's ten second one are (1/40)^20.
	require.NotZero(t, beyondFreshCeiling,
		"an aged row must back off on its own attempt count, not the batch minimum")

	// Jitter is per row for the same reason the delay is: one timestamp for the
	// batch makes every row eligible again at the same instant, which is the
	// thundering herd the jitter exists to break up.
	require.Greater(t, len(windows), 1, "each retrying row must get its own jittered retry window")
}

// TestDrain_UnknownTopicRetries pins the rolling-deploy contract: writers
// validate names against the same registry, so a topic unknown to this drainer
// was declared by a binary newer than it — a condition that clears when the
// drainer is redeployed. Dead-lettering here would turn every deploy that adds
// a topic into silent event loss for rows written during the skew window.
func TestDrain_UnknownTopicRetries(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	row := seedRow(t, inst.conn, orgID, seedOptions{topic: "gram.newer.v1.Event"})

	inst.pub.failWith = func(string, int) error {
		return topics.ErrUnknownTopic
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, result.DeadLettered, "a topic unknown to this binary may be known to the next one")
	require.Equal(t, 1, result.Retrying)

	require.Equal(t, int64(1), countRows(t, inst.conn))

	stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), row.ID)
	require.NoError(t, err)
	require.True(t, stored.RetryAfter.Valid, "the row must wait out the deploy, not fail out of it")
	require.Contains(t, stored.LastError.String, "unknown pubsub topic")

	require.Empty(t, inst.pub.messages(),
		"an unresolved topic never reaches Pub/Sub, so nothing was delivered")
}

// TestDrain_DeadLettersRecordEachRowsOwnError pins the recorded error to the
// row it belongs to. One batch can hold rows failing permanently for different
// reasons, and the dead letter table is the only surviving account of why a
// row was given up on — a row stamped with its neighbour's failure sends
// whoever triages it after a problem that row does not have.
func TestDrain_DeadLettersRecordEachRowsOwnError(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	rows := []testrepo.SeedPublishOutboxRowRow{
		seedRow(t, inst.conn, orgID, seedOptions{topic: "gram.alpha.v1.Event"}),
		seedRow(t, inst.conn, orgID, seedOptions{topic: "gram.beta.v1.Event"}),
	}

	// The relay names the topic it could not reach in the error it records, so
	// two permanently failing rows in one batch produce two distinct messages.
	inst.pub.failWith = func(string, int) error {
		return oops.Permanent(errors.New("schema rejected"))
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, result.DeadLettered)

	for _, row := range rows {
		dead, err := testrepo.New(inst.conn).GetPublishOutboxDeadLetter(t.Context(), row.PublicID)
		require.NoError(t, err)
		require.Contains(t, dead.LastError, dead.Topic,
			"a dead letter must record why its own row failed, not why the last row in the batch did")
	}
}

func TestDrain_ExhaustedAttemptsDeadLetters(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	// One below the cap: the claim bumps it to the cap, so this drain is the
	// last one the row gets.
	row := seedRow(t, inst.conn, orgID, seedOptions{attempts: 9})

	inst.pub.failWith = func(string, int) error {
		return errors.New("still unavailable")
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.DeadLettered)
	require.Equal(t, 0, result.Retrying)

	dead, err := testrepo.New(inst.conn).GetPublishOutboxDeadLetter(t.Context(), row.PublicID)
	require.NoError(t, err)
	require.Equal(t, int32(10), dead.Attempts)
}

func TestDrain_PartialBatchSettlesEachRowIndependently(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	rows := make([]testrepo.SeedPublishOutboxRowRow, 0, 3)
	for range 3 {
		rows = append(rows, seedRow(t, inst.conn, orgID, seedOptions{}))
	}

	// Fail only the second publish. The other two must still be deleted — a
	// batch that settles all-or-nothing would either lose or duplicate events.
	inst.pub.failWith = func(_ string, call int) error {
		if call == 1 {
			return errors.New("transient")
		}
		return nil
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, result.Published)
	require.Equal(t, 1, result.Retrying)

	require.Equal(t, int64(1), countRows(t, inst.conn))

	stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), rows[1].ID)
	require.NoError(t, err)
	require.True(t, stored.RetryAfter.Valid)

	for _, idx := range []int{0, 2} {
		_, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), rows[idx].ID)
		require.ErrorIs(t, err, pgx.ErrNoRows, "successfully published rows must be gone")
	}

	// Two deliveries for three attempts. The failed one still advanced the call
	// index that selected it, so this also pins that attempts and deliveries
	// are counted separately.
	require.Len(t, inst.pub.messages(), 2,
		"the retained row must not be recorded as delivered")
}

func TestDrain_SkipsRowsCoolingOff(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	future := time.Now().Add(time.Hour)
	seedRow(t, inst.conn, orgID, seedOptions{retryAfter: &future})

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, result.Published)
	require.Empty(t, inst.pub.messages())
	require.Equal(t, int64(1), countRows(t, inst.conn))
}

func TestDrain_SkipsRowsLeasedByAnotherDrainer(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	future := time.Now().Add(time.Hour)
	seedRow(t, inst.conn, orgID, seedOptions{lockedUntil: &future})

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, result.Published)
	require.Empty(t, inst.pub.messages())
}

func TestDrain_ReclaimsRowsWithExpiredLease(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	past := time.Now().Add(-time.Hour)
	seedRow(t, inst.conn, orgID, seedOptions{lockedUntil: &past})

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.Published, "a drainer that died mid-batch must not strand its rows")
}

func TestDrain_HasMoreProbeDoesNotStrandTheSurplusRow(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	// One more than a full batch so the N+1 probe fires.
	for range 51 {
		seedRow(t, inst.conn, orgID, seedOptions{})
	}

	first, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 50, first.Published)
	require.True(t, first.HasMore)

	// The probe row was claimed too. If its lease were left in place it would be
	// invisible for the next minute; the relay has to hand it back.
	second, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, second.Published)
	require.False(t, second.HasMore)
	require.Equal(t, int64(0), countRows(t, inst.conn))
}

// TestDrain_ConcurrentDrainersClaimDisjointRows is the test that justifies the
// FOR UPDATE SKIP LOCKED claim. Without it two drainers would both read the
// same pending rows and publish each one twice.
func TestDrain_ConcurrentDrainersClaimDisjointRows(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)

	const rowCount = 40
	for range rowCount {
		seedRow(t, inst.conn, orgID, seedOptions{})
	}

	// A second relay over the same database, standing in for a second worker.
	other := publishOutboxRelayOver(t, inst.conn)

	var wg sync.WaitGroup
	results := make([]int, 2)
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		res, err := inst.relay.Drain(t.Context())
		results[0], errs[0] = res.Published, err
	}()
	go func() {
		defer wg.Done()
		res, err := other.relay.Drain(t.Context())
		results[1], errs[1] = res.Published, err
	}()
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	require.Equal(t, rowCount, results[0]+results[1], "every row must be published exactly once")
	require.Equal(t, int64(0), countRows(t, inst.conn))

	seen := map[string]int{}
	for _, msg := range append(inst.pub.messages(), other.pub.messages()...) {
		seen[string(msg.Data)]++
	}
	for data, count := range seen {
		require.Equalf(t, 1, count, "message %q was published %d times", data, count)
	}
}

// TestClaimStampsOneTokenAcrossTheBatch is what lets settlement carry a single
// token rather than one per row. It holds because the caller supplies the
// token; generating it in SQL with gen_random_uuid() would evaluate per row and
// hand every row a different one, silently fencing only the first.
func TestClaimStampsOneTokenAcrossTheBatch(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	for range 5 {
		seedRow(t, inst.conn, orgID, seedOptions{})
	}

	token := uuid.New()
	claimed, err := repo.New(inst.conn).ClaimPublishOutboxBatch(t.Context(), repo.ClaimPublishOutboxBatchParams{
		Lease:      pgtype.Interval{Microseconds: time.Minute.Microseconds(), Days: 0, Months: 0, Valid: true},
		LeaseToken: token,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 5)

	for _, row := range claimed {
		stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), row.ID)
		require.NoError(t, err)
		require.True(t, stored.LeaseToken.Valid)
		require.Equal(t, token, stored.LeaseToken.UUID,
			"every row in one claim must carry the same token, or settlement fences only some of them")
	}
}

// TestSettlementIgnoresRowsReclaimedAfterLeaseExpiry is the reason settlement
// carries the lease it was granted rather than just the row id.
//
// A drain that outlives its own lease is not hypothetical: the publish timeout
// alone is 30s of the 60s lease, and a stalled worker or a slow settle can push
// past it. By then another drainer may hold the row, and settling on the id
// would let the late result act on someone else's claim — clearing a live
// lease so a third worker publishes the same event, or dead-lettering a row
// that is about to be delivered perfectly well.
//
// The stale release is the worst of them: it decrements attempts, which is the
// counter the dead-letter threshold acts on, so a row could be walked backwards
// away from ever giving up.
func TestSettlementIgnoresRowsReclaimedAfterLeaseExpiry(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	row := seedRow(t, inst.conn, orgID, seedOptions{})

	q := repo.New(inst.conn)
	ids := []int64{row.ID}

	// A zero lease stands in for one that has already elapsed by the time this
	// drain gets around to settling.
	staleToken := uuid.New()
	stale, err := q.ClaimPublishOutboxBatch(t.Context(), repo.ClaimPublishOutboxBatchParams{
		Lease:      pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: true},
		LeaseToken: staleToken,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Len(t, stale, 1)

	// A second drainer finds the lease expired and takes the row over.
	liveToken := uuid.New()
	live, err := q.ClaimPublishOutboxBatch(t.Context(), repo.ClaimPublishOutboxBatchParams{
		Lease:      pgtype.Interval{Microseconds: time.Minute.Microseconds(), Days: 0, Months: 0, Valid: true},
		LeaseToken: liveToken,
		BatchSize:  10,
	})
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, row.ID, live[0].ID)

	// Every settlement the late drain could reach, all bearing its dead lease.
	deleted, err := q.DeletePublishedOutboxRows(t.Context(), repo.DeletePublishedOutboxRowsParams{
		Ids:        ids,
		LeaseToken: staleToken,
	})
	require.NoError(t, err)
	require.Zero(t, deleted)

	deadLettered, err := q.DeadLetterPublishOutboxRows(t.Context(), repo.DeadLetterPublishOutboxRowsParams{
		Ids:        ids,
		Errors:     []string{"stale"},
		LeaseToken: staleToken,
	})
	require.NoError(t, err)
	require.Zero(t, deadLettered)

	require.NoError(t, q.MarkPublishOutboxFailed(t.Context(), repo.MarkPublishOutboxFailedParams{
		Ids:    ids,
		Errors: []string{"stale"},
		RetryAfters: []pgtype.Timestamptz{
			{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		},
		LeaseToken: staleToken,
	}))

	require.NoError(t, q.ReleasePublishOutboxRows(t.Context(), repo.ReleasePublishOutboxRowsParams{
		Ids:        ids,
		LeaseToken: staleToken,
	}))

	// The live claim is untouched: still leased, still pending, still counting
	// the attempts it has actually made.
	stored, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), countRows(t, inst.conn), "the row belongs to the live claim")
	require.Equal(t, live[0].Attempts, stored.Attempts,
		"a stale release must not walk back the attempt count the dead-letter threshold reads")
	require.True(t, stored.LockedUntil.Valid, "a stale settle must not release a live lease")
	require.Equal(t, liveToken, stored.LeaseToken.UUID, "the live claim still owns the row")
	require.False(t, stored.RetryAfter.Valid, "a stale failure must not schedule a retry")
	require.Empty(t, stored.LastError.String)
}
