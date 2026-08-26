package replyinbox

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
)

var (
	gitleaksLane = Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS, PolicyID: ""}
	presidioLane = Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO, PolicyID: ""}
)

type awaitResult struct {
	outcome Outcome
	err     error
}

func testReply(scanID string, lane Lane, status riskv1.EnforcementStatus) *riskv1.EnforcementReply {
	return riskv1.EnforcementReply_builder{
		ScanId:   new(scanID),
		Scanner:  new(lane.Scanner),
		Status:   new(status),
		PolicyId: new(lane.PolicyID),
	}.Build()
}

func TestAwaitHappyPath(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-a")
	result := make(chan awaitResult, 1)
	go func() {
		outcome, err := te.inbox.Await(t.Context(), "scan-a", []Lane{gitleaksLane})
		result <- awaitResult{outcome: outcome, err: err}
	}()
	waitForWaiter(t, te.inbox, "scan-a")
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-a"), testReply("scan-a", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.True(t, got.outcome.Complete)
	require.False(t, got.outcome.Deadline)
	require.Equal(t, "scan-a", got.outcome.ByLane[gitleaksLane].GetScanId())
}

func TestAwaitDeadlineReturnsPartialOutcome(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-partial")
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	result := make(chan awaitResult, 1)
	go func() {
		outcome, err := te.inbox.Await(ctx, "scan-partial", []Lane{gitleaksLane, presidioLane})
		result <- awaitResult{outcome: outcome, err: err}
	}()
	waitForWaiter(t, te.inbox, "scan-partial")
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-partial"), testReply("scan-partial", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.False(t, got.outcome.Complete)
	require.True(t, got.outcome.Deadline)
	require.Len(t, got.outcome.ByLane, 1)
	require.NotNil(t, got.outcome.ByLane[gitleaksLane])
}

func TestAwaitDeduplicatesScannerRedelivery(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dedupe")
	result := make(chan awaitResult, 1)
	go func() {
		outcome, err := te.inbox.Await(t.Context(), "scan-dedupe", []Lane{gitleaksLane, presidioLane})
		result <- awaitResult{outcome: outcome, err: err}
	}()
	waitForWaiter(t, te.inbox, "scan-dedupe")
	first := testReply("scan-dedupe", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-dedupe"), first))
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-dedupe"), testReply("scan-dedupe", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR)))
	require.Never(t, func() bool {
		return len(result) > 0
	}, 50*time.Millisecond, 5*time.Millisecond)
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-dedupe"), testReply("scan-dedupe", presidioLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.True(t, got.outcome.Complete)
	require.Len(t, got.outcome.ByLane, 2)
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, got.outcome.ByLane[gitleaksLane].GetStatus())
}

func TestAwaitKeysJudgeLanesByPolicy(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-judge")
	judgeOne := Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE, PolicyID: "policy-one"}
	judgeTwo := Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE, PolicyID: "policy-two"}
	result := make(chan awaitResult, 1)
	go func() {
		outcome, err := te.inbox.Await(t.Context(), "scan-judge", []Lane{judgeOne, judgeTwo})
		result <- awaitResult{outcome: outcome, err: err}
	}()
	waitForWaiter(t, te.inbox, "scan-judge")
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-judge"), testReply("scan-judge", judgeOne, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-judge"), testReply("scan-judge", judgeTwo, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.True(t, got.outcome.Complete)
	require.Len(t, got.outcome.ByLane, 2)
}

func TestOrphanReplyIsDroppedAndCounted(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-orphan")
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-orphan"), testReply("scan-orphan", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

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

func TestReplyAfterReleaseIsCountedAsOrphan(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-released-waiter")
	_, release, err := te.inbox.register("scan-released", []Lane{gitleaksLane})
	require.NoError(t, err)
	release()

	reply, err := proto.Marshal(testReply("scan-released", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	require.NoError(t, err)
	te.inbox.route(t.Context(), string(reply))

	require.Equal(t, uint64(1), te.inbox.Snapshot().OrphanedReplies)
}

func TestReplyOverflowingWaiterBufferIsCountedAsOrphan(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-overflow")
	w, release, err := te.inbox.register("scan-overflow", []Lane{gitleaksLane})
	require.NoError(t, err)
	defer release()

	reply, err := proto.Marshal(testReply("scan-overflow", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	require.NoError(t, err)
	for range cap(w.done) + 1 {
		te.inbox.route(t.Context(), string(reply))
	}

	require.Len(t, w.done, cap(w.done))
	require.Equal(t, uint64(1), te.inbox.Snapshot().OrphanedReplies)
}

func TestReplicaInboxesDoNotCross(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-one")
	inboxTwo, err := New(t.Context(), newTestLogger(), otel.GetTracerProvider(), otel.GetMeterProvider(), Config{
		RedisOptions: redis.Options{Addr: te.redis.Addr(), Protocol: 2},
		ReplicaID:    "replica-two",
		BlockTimeout: time.Second,
		DrainGate:    nil,
		drainFunc:    nil,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = inboxTwo.Close() })

	ctxTwo, cancelTwo := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancelTwo()
	resultOne := make(chan awaitResult, 1)
	resultTwo := make(chan awaitResult, 1)
	go func() {
		outcome, awaitErr := te.inbox.Await(t.Context(), "same-scan", []Lane{gitleaksLane})
		resultOne <- awaitResult{outcome: outcome, err: awaitErr}
	}()
	go func() {
		outcome, awaitErr := inboxTwo.Await(ctxTwo, "same-scan", []Lane{gitleaksLane})
		resultTwo <- awaitResult{outcome: outcome, err: awaitErr}
	}()
	waitForWaiter(t, te.inbox, "same-scan")
	waitForWaiter(t, inboxTwo, "same-scan")
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("same-scan"), testReply("same-scan", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	one := <-resultOne
	require.NoError(t, one.err)
	require.True(t, one.outcome.Complete)
	two := <-resultTwo
	require.NoError(t, two.err)
	require.True(t, two.outcome.Deadline)
	require.Empty(t, two.outcome.ByLane)
}

func TestInvalidReplicaIDIsRejected(t *testing.T) {
	t.Parallel()

	_, err := New(t.Context(), newTestLogger(), otel.GetTracerProvider(), otel.GetMeterProvider(), Config{
		RedisOptions: redis.Options{Addr: "127.0.0.1:1"},
		ReplicaID:    "replica:unsafe",
		BlockTimeout: time.Second,
		DrainGate:    nil,
		drainFunc:    nil,
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
		outcome, err := te.inbox.Await(ctx, "scan-reconnect", []Lane{gitleaksLane})
		result <- awaitResult{outcome: outcome, err: err}
	}()
	waitForWaiter(t, te.inbox, "scan-reconnect")

	staleClient := te.inbox.redisClient()
	te.redis.Close()
	require.NoError(t, te.client.Close())
	require.Eventually(t, func() bool {
		return te.inbox.redisClient() != staleClient
	}, 5*time.Second, 5*time.Millisecond)
	miniCtx, miniCancel := context.WithCancel(t.Context())
	t.Cleanup(miniCancel)
	// Restart does not renew miniredis's canceled blocking-command context.
	te.redis.Ctx = miniCtx
	te.redis.CtxCancel = miniCancel
	require.NoError(t, te.redis.Restart())
	require.Equal(t, te.inbox.client.Options().Addr, te.redis.Addr())
	te.client = redis.NewClient(&redis.Options{Addr: te.redis.Addr(), Protocol: 2})
	t.Cleanup(func() { _ = te.client.Close() })
	te.writer = NewWriter(te.client)
	require.NoError(t, te.writer.Write(ctx, te.inbox.ReplyURN("scan-reconnect"), testReply("scan-reconnect", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))

	got := <-result
	require.NoError(t, got.err)
	require.True(t, got.outcome.Complete)
}

func TestDedicatedClientUsesBoundedPool(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-options")
	require.Equal(t, defaultPoolSize, te.inbox.client.Options().PoolSize)
}

func TestDrainerSupervisorRestartsAfterPanic(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	run := func(ctx context.Context) {
		if attempts.Add(1) == 1 {
			panic("synthetic drainer panic")
		}
		<-ctx.Done()
	}
	te := setupInboxTestWithDrainer(t, "replica-supervised", run)

	require.Eventually(t, func() bool {
		stats := te.inbox.Snapshot()
		return attempts.Load() >= 2 && stats.DrainerAlive && stats.DrainerErrors == 1
	}, 2*time.Second, 5*time.Millisecond)

	var metrics metricdata.ResourceMetrics
	require.NoError(t, te.reader.Collect(t.Context(), &metrics))
	require.Equal(t, int64(1), int64MetricValue(t, metrics, "risk.enforcement.drainer_alive"))
	require.Equal(t, int64(1), int64MetricValue(t, metrics, "risk.enforcement.drainer_errors"))
}

func TestDrainGateExposesBacklogAndDrainStats(t *testing.T) {
	t.Parallel()

	drainGate := make(chan struct{})
	te := setupInboxTestWithDrainGate(t, "replica-gated", drainGate)
	result := make(chan awaitResult, 1)
	go func() {
		outcome, err := te.inbox.Await(t.Context(), "scan-gated", []Lane{gitleaksLane})
		result <- awaitResult{outcome: outcome, err: err}
	}()
	waitForWaiter(t, te.inbox, "scan-gated")
	require.NoError(t, te.writer.Write(t.Context(), te.inbox.ReplyURN("scan-gated"), testReply("scan-gated", gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK)))
	require.Eventually(t, func() bool {
		values, err := te.redis.List(te.inbox.key)
		return err != nil || len(values) == 0
	}, time.Second, 5*time.Millisecond)

	paused := te.inbox.Snapshot()
	require.Equal(t, 1, paused.Waiters)
	require.Zero(t, paused.DrainedReplies)
	close(drainGate)

	got := <-result
	require.NoError(t, got.err)
	require.True(t, got.outcome.Complete)
	require.Eventually(t, func() bool {
		return te.inbox.Snapshot().DrainedReplies == 1
	}, time.Second, 5*time.Millisecond)
	drained := te.inbox.Snapshot()
	require.Equal(t, uint64(1), drained.DrainBatches)
	require.Equal(t, uint64(1), drained.MaxDrainBatch)
	require.Equal(t, uint64(0), drained.OrphanedReplies)
	require.NotNil(t, drained.RedisPool)
}

func int64MetricValue(t *testing.T, metrics metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			switch data := candidate.Data.(type) {
			case metricdata.Gauge[int64]:
				require.Len(t, data.DataPoints, 1)
				return data.DataPoints[0].Value
			case metricdata.Sum[int64]:
				require.Len(t, data.DataPoints, 1)
				return data.DataPoints[0].Value
			default:
				require.Fail(t, "unexpected metric type", name)
			}
		}
	}
	require.Fail(t, "metric not found", name)
	return 0
}
