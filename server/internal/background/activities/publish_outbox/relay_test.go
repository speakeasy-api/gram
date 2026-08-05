package publish_outbox_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
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
	require.Equal(t, protoreflect.FullName(webhooksTopic), published[0].Topic)
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

	inst.pub.failWith = func(protoreflect.FullName, int) error {
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
}

func TestDrain_UnknownTopicDeadLetters(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	row := seedRow(t, inst.conn, orgID, seedOptions{topic: "gram.nope.v1.Missing"})

	inst.pub.failWith = func(protoreflect.FullName, int) error {
		return gcp.ErrUnknownTopic
	}

	result, err := inst.relay.Drain(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, result.DeadLettered)
	require.Equal(t, 0, result.Retrying, "an unresolvable topic will not resolve itself on retry")

	require.Equal(t, int64(0), countRows(t, inst.conn))

	dead, err := testrepo.New(inst.conn).GetPublishOutboxDeadLetter(t.Context(), row.PublicID)
	require.NoError(t, err)
	require.Equal(t, "gram.nope.v1.Missing", dead.Topic)
	require.Contains(t, dead.LastError, "unknown pubsub topic")
	require.True(t, dead.EnqueuedAt.Valid, "the original enqueue time must survive the move")
}

func TestDrain_ExhaustedAttemptsDeadLetters(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	orgID := seedOrg(t, inst.conn)
	// One below the cap: the claim bumps it to the cap, so this drain is the
	// last one the row gets.
	row := seedRow(t, inst.conn, orgID, seedOptions{attempts: 9})

	inst.pub.failWith = func(protoreflect.FullName, int) error {
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
	inst.pub.failWith = func(_ protoreflect.FullName, call int) error {
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
