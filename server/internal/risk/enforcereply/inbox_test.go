package enforcereply

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
)

var (
	gitleaksLane = Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS, PolicyID: ""}
	presidioLane = Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO, PolicyID: ""}
)

type awaitResult struct {
	reply *riskv1.EnforcementReply
	err   error
}

func testReply(correlationID string, lane Lane, status riskv1.EnforcementStatus) *riskv1.EnforcementReply {
	return riskv1.EnforcementReply_builder{
		CorrelationId: new(correlationID),
		Scanner:       new(lane.Scanner),
		Status:        new(status),
		PolicyId:      new(lane.PolicyID),
	}.Build()
}

func TestAwaitHappyPath(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-a")
	result := make(chan awaitResult, 1)
	go func() {
		reply, err := te.inbox.Await(t.Context(), "correlation-a")
		result <- awaitResult{reply: reply, err: err}
	}()
	waitForWaiter(t, te.inbox, "correlation-a")
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("correlation-a"), testReply("correlation-a", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.Equal(t, "correlation-a", got.reply.GetCorrelationId())
}

func TestAwaitDeadlineReturnsContextError(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-deadline")
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	reply, err := te.inbox.Await(ctx, "correlation-deadline")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, reply)
	require.Zero(t, te.inbox.Snapshot().Waiters)
}

func TestFirstReplyWinsAndDuplicateIsOrphaned(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dedupe")
	w, release, err := te.inbox.Register("correlation-dedupe")
	require.NoError(t, err)
	defer release()
	first := testReply("correlation-dedupe", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)
	second := testReply("correlation-dedupe", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR)
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("correlation-dedupe"), first))
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("correlation-dedupe"), second))

	got, err := te.inbox.AwaitRegistered(t.Context(), "correlation-dedupe", w, time.Now())
	require.NoError(t, err)
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, got.GetStatus())
	require.Eventually(t, func() bool {
		return te.inbox.Snapshot().OrphanedReplies == 1
	}, time.Second, 5*time.Millisecond)
}

func TestOrphanReplyIsDroppedAndCounted(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-orphan")
	_, release, err := te.inbox.Register("correlation-live")
	require.NoError(t, err)
	defer release()
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("correlation-orphan"), testReply("correlation-orphan", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	require.Eventually(t, func() bool {
		var metrics metricdata.ResourceMetrics
		if err := te.reader.Collect(t.Context(), &metrics); err != nil {
			return false
		}
		for _, scope := range metrics.ScopeMetrics {
			for _, candidate := range scope.Metrics {
				if candidate.Name != "risk.enforcement.orphaned_replies" {
					continue
				}
				sum, ok := candidate.Data.(metricdata.Sum[int64])
				return ok && len(sum.DataPoints) == 1 && sum.DataPoints[0].Value == 1
			}
		}
		return false
	}, time.Second, 5*time.Millisecond)
}

func TestReplicaInboxesDoNotCross(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-one")
	inboxTwo, err := New(t.Context(), newTestLogger(), otel.GetTracerProvider(), otel.GetMeterProvider(), Config{
		RedisOptions: redis.Options{Addr: te.redis.Addr(), Protocol: 2},
		ReplicaID:    "replica-two",
		PollInterval: DefaultPollInterval,
		DrainGate:    nil,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = inboxTwo.Close() })

	ctxTwo, cancelTwo := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelTwo()
	resultOne := make(chan awaitResult, 1)
	resultTwo := make(chan awaitResult, 1)
	go func() {
		reply, awaitErr := te.inbox.Await(t.Context(), "same-correlation")
		resultOne <- awaitResult{reply: reply, err: awaitErr}
	}()
	go func() {
		reply, awaitErr := inboxTwo.Await(ctxTwo, "same-correlation")
		resultTwo <- awaitResult{reply: reply, err: awaitErr}
	}()
	waitForWaiter(t, te.inbox, "same-correlation")
	waitForWaiter(t, inboxTwo, "same-correlation")
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("same-correlation"), testReply("same-correlation", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	one := <-resultOne
	require.NoError(t, one.err)
	require.NotNil(t, one.reply)
	two := <-resultTwo
	require.ErrorIs(t, two.err, context.DeadlineExceeded)
	require.Nil(t, two.reply)
}

func TestInvalidReplicaIDIsRejected(t *testing.T) {
	t.Parallel()

	_, err := New(t.Context(), newTestLogger(), otel.GetTracerProvider(), otel.GetMeterProvider(), Config{
		RedisOptions: redis.Options{Addr: "127.0.0.1:1"},
		ReplicaID:    "replica:unsafe",
		PollInterval: DefaultPollInterval,
		DrainGate:    nil,
	})
	require.ErrorContains(t, err, "invalid reply inbox replica id")
}

func TestDrainerReconnectsAndDrainsQueuedReply(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-reconnect")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan awaitResult, 1)
	go func() {
		reply, err := te.inbox.Await(ctx, "correlation-reconnect")
		result <- awaitResult{reply: reply, err: err}
	}()
	waitForWaiter(t, te.inbox, "correlation-reconnect")

	te.redis.Close()
	require.NoError(t, te.client.Close())
	require.NoError(t, te.redis.Restart())
	te.client = redis.NewClient(&redis.Options{Addr: te.redis.Addr(), Protocol: 2})
	t.Cleanup(func() { _ = te.client.Close() })
	te.writer = NewWriter(te.client)
	require.NoError(t, te.writer.Reply(ctx, te.inbox.URN("correlation-reconnect"), testReply("correlation-reconnect", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.NotNil(t, got.reply)
}

func TestDrainGateExposesBacklogAndDrainStats(t *testing.T) {
	t.Parallel()

	drainGate := make(chan struct{})
	te := setupInboxTestWithDrainGate(t, "replica-gated", drainGate)
	result := make(chan awaitResult, 1)
	go func() {
		reply, err := te.inbox.Await(t.Context(), "correlation-gated")
		result <- awaitResult{reply: reply, err: err}
	}()
	waitForWaiter(t, te.inbox, "correlation-gated")
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("correlation-gated"), testReply("correlation-gated", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))
	require.Eventually(t, func() bool {
		values, err := te.redis.List(InboxKey("replica-gated"))
		return err != nil || len(values) == 0
	}, time.Second, 5*time.Millisecond)

	paused := te.inbox.Snapshot()
	require.Equal(t, 1, paused.Waiters)
	require.Zero(t, paused.DrainedReplies)
	close(drainGate)

	got := <-result
	require.NoError(t, got.err)
	require.NotNil(t, got.reply)
	require.Eventually(t, func() bool {
		return te.inbox.Snapshot().DrainedReplies == 1
	}, time.Second, 5*time.Millisecond)
	drained := te.inbox.Snapshot()
	require.Equal(t, uint64(1), drained.DrainBatches)
	require.Equal(t, uint64(1), drained.MaxDrainBatch)
	require.Equal(t, uint64(0), drained.OrphanedReplies)
	require.NotNil(t, drained.RedisPool)
}

func TestWriterRejectsMismatchedCorrelation(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-mismatch")
	err := te.writer.Reply(t.Context(), te.inbox.URN("expected"), testReply("other", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	require.Error(t, err)
	require.NotErrorIs(t, err, context.Canceled)
}
