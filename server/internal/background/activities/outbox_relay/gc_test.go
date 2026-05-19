package outbox_relay_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay"
	bgactivitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type gcTestInstance struct {
	conn *pgxpool.Pool
	gc   *outbox_relay.GC
}

func newGCTestInstance(t *testing.T) *gcTestInstance {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	return &gcTestInstance{conn: conn, gc: outbox_relay.NewGC(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn)}
}

func TestGCDeletesTerminalRows(t *testing.T) {
	t.Parallel()

	inst := newGCTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-gc", true)
	q := bgactivitiesrepo.New(inst.conn)

	// Row 1: processed (successfully delivered).
	processedID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	err := q.MarkOutboxRelayProcessed(ctx, bgactivitiesrepo.MarkOutboxRelayProcessedParams{
		OutboxID:      processedID,
		SvixMessageID: conv.ToPGTextEmpty("svix-123"),
	})
	require.NoError(t, err)

	// Row 2: dead-lettered (retry budget exhausted).
	deadID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	err = q.MarkOutboxRelayDeadLettered(ctx, bgactivitiesrepo.MarkOutboxRelayDeadLetteredParams{
		OutboxID:  deadID,
		LastError: conv.ToPGTextEmpty("permanent error"),
	})
	require.NoError(t, err)

	// Row 3: pending — no relay row yet, must survive.
	pendingID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	// cutoff is in the future so all rows created now are eligible by age.
	deleted, err := inst.gc.DeleteProcessedRows(ctx, time.Now().Add(time.Hour), 500)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)

	tr := testrepo.New(inst.conn)

	_, err = tr.GetOutboxEntry(ctx, processedID)
	require.Error(t, err, "processed row should be gone")

	_, err = tr.GetOutboxEntry(ctx, deadID)
	require.Error(t, err, "dead-lettered row should be gone")

	id, err := tr.GetOutboxEntry(ctx, pendingID)
	require.NoError(t, err, "pending row must survive")
	require.Equal(t, pendingID, id)
}

func TestGCRetainsRecentRows(t *testing.T) {
	t.Parallel()

	inst := newGCTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-gc-recent", true)
	q := bgactivitiesrepo.New(inst.conn)

	id1 := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	id2 := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	for _, id := range []int64{id1, id2} {
		err := q.MarkOutboxRelayProcessed(ctx, bgactivitiesrepo.MarkOutboxRelayProcessedParams{
			OutboxID:      id,
			SvixMessageID: conv.ToPGTextEmpty("svix-ok"),
		})
		require.NoError(t, err)
	}

	// Cutoff 24 hours ago — rows were just created, so they are within retention.
	deleted, err := inst.gc.DeleteProcessedRows(ctx, time.Now().Add(-24*time.Hour), 500)
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted)

	tr := testrepo.New(inst.conn)
	for _, id := range []int64{id1, id2} {
		_, err := tr.GetOutboxEntry(ctx, id)
		require.NoError(t, err)
	}
}

func TestGCRespectsMaxBatchSize(t *testing.T) {
	t.Parallel()

	inst := newGCTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-gc-batch", true)
	q := bgactivitiesrepo.New(inst.conn)

	for range 10 {
		id := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
		err := q.MarkOutboxRelayProcessed(ctx, bgactivitiesrepo.MarkOutboxRelayProcessedParams{
			OutboxID:      id,
			SvixMessageID: conv.ToPGTextEmpty("svix-ok"),
		})
		require.NoError(t, err)
	}

	deleted, err := inst.gc.DeleteProcessedRows(ctx, time.Now().Add(time.Hour), 5)
	require.NoError(t, err)
	require.Equal(t, int64(5), deleted)
}

func TestFetchEvents_SkipsCoolingOffRows(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-backoff", true)
	coolingID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	eligibleID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	q := bgactivitiesrepo.New(inst.conn)
	err := q.MarkOutboxRelayFailed(ctx, bgactivitiesrepo.MarkOutboxRelayFailedParams{
		OutboxID:   coolingID,
		LastError:  conv.ToPGTextEmpty("transient"),
		RetryAfter: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, eligibleID, result.Events[0].OutboxID)
}

func TestFetchEvents_IncludesRowAfterCooldown(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-backoff-expired", true)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	q := bgactivitiesrepo.New(inst.conn)
	err := q.MarkOutboxRelayFailed(ctx, bgactivitiesrepo.MarkOutboxRelayFailedParams{
		OutboxID:   outboxID,
		LastError:  conv.ToPGTextEmpty("transient"),
		RetryAfter: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, outboxID, result.Events[0].OutboxID)
}
