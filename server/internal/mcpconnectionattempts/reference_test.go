package mcpconnectionattempts

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func withSlot(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.WithConnectionReferenceSlot(t.Context())
}

func TestReference_IsTheStoredRowsID(t *testing.T) {
	t.Parallel()

	var rows []Row
	recorder := NewCapturing(func(row Row) { rows = append(rows, row) }, logsOn)
	ctx := withSlot(t)

	recorder.Once(ctx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)

	require.Len(t, rows, 1)
	require.Equal(t, rows[0].AttemptID.String(), contextvalues.ConnectionReference(ctx))
}

func TestReference_NotShownWhenLogsAreOff(t *testing.T) {
	t.Parallel()

	offCtx := withSlot(t)
	NewCapturing(func(Row) {}, func(context.Context, string) (bool, error) { return false, nil }).Once(offCtx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)
	require.Empty(t, contextvalues.ConnectionReference(offCtx))

	noCheckCtx := withSlot(t)
	NewCapturing(func(Row) {}, nil).Once(noCheckCtx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)
	require.Empty(t, contextvalues.ConnectionReference(noCheckCtx))
}

func TestReference_NotShownWhenTheLogsCheckFails(t *testing.T) {
	t.Parallel()

	ctx := withSlot(t)
	conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
	recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, func(context.Context, string) (bool, error) {
		return false, errors.New("logs check failed")
	}, testConfig())
	t.Cleanup(func() { _ = recorder.Shutdown(context.WithoutCancel(ctx)) })

	recorder.Once(ctx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)
	require.Empty(t, contextvalues.ConnectionReference(ctx))
}

func TestReference_ALogsAnswerIsCachedForTheFailureLoop(t *testing.T) {
	t.Parallel()

	check := &logsCheck{enabled: true, err: nil, block: nil, panics: false, checks: atomic.Int64{}}
	conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
	recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, check.check, testConfig())
	t.Cleanup(func() { _ = recorder.Shutdown(context.WithoutCancel(t.Context())) })

	for range 5 {
		require.True(t, recorder.logsAllowForReference(t.Context(), "org"))
	}
	require.Equal(t, int64(1), check.checks.Load(), "only reference checks run here: no row was queued")
}

// A stalled logs lookup must not hold the failure response; the answer serves the next one.
func TestReference_ASlowLogsCheckShowsNoReferenceAndDoesNotBlock(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		block := make(chan struct{})
		check := &logsCheck{enabled: true, err: nil, block: block, panics: false, checks: atomic.Int64{}}
		conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
		recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, check.check, testConfig())

		start := time.Now()
		var wg sync.WaitGroup
		results := make([]bool, 3)
		for i := range results {
			wg.Go(func() { results[i] = recorder.logsAllowForReference(t.Context(), "org") })
		}
		wg.Wait()
		require.Equal(t, referenceLogsTimeout, time.Since(start), "the slow path waits only the bound")
		require.Equal(t, []bool{false, false, false}, results)
		require.Equal(t, int64(1), check.checks.Load(), "one background check for the organization")

		close(block)
		synctest.Wait()
		require.True(t, recorder.logsAllowForReference(t.Context(), "org"), "the finished check answers the next failure")
		require.NoError(t, recorder.Shutdown(t.Context()))
	})
}

func TestReference_NonFailuresLeaveNone(t *testing.T) {
	t.Parallel()

	for _, what := range []Happened{CodeRefreshedUnverified, CodeCanceled, CodeReviewCanceled} {
		ctx := withSlot(t)
		NewCapturing(func(Row) {}, logsOn).Once(ctx, testScope(), StepMCPRequest, what)
		require.Empty(t, contextvalues.ConnectionReference(ctx))
	}
}

func TestReference_AFailedRefreshIsNotShownUntilClaimed(t *testing.T) {
	t.Parallel()

	var rows []Row
	recorder := NewCapturing(func(row Row) { rows = append(rows, row) }, logsOn)
	ctx := withSlot(t)

	recorder.Once(ctx, testScope(), StepProviderRefresh, CodeRefreshRejected)
	require.Empty(t, contextvalues.ConnectionReference(ctx), "an error unrelated to the refresh must not show it")

	contextvalues.ClaimRefreshConnectionReference(ctx)
	require.Equal(t, rows[0].AttemptID.String(), contextvalues.ConnectionReference(ctx))
}

func TestReference_SeveralFailedRefreshesCannotBeClaimed(t *testing.T) {
	t.Parallel()

	recorder := NewCapturing(func(Row) {}, logsOn)
	ctx := withSlot(t)
	recorder.Once(ctx, testScope(), StepProviderRefresh, CodeRefreshRejected)
	recorder.Once(ctx, testScope(), StepProviderRefresh, CodeRefreshRejected)

	contextvalues.ClaimRefreshConnectionReference(ctx)
	require.Empty(t, contextvalues.ConnectionReference(ctx))
}

func TestReference_DroppedRowsLeaveNone(t *testing.T) {
	t.Parallel()

	ctx := withSlot(t)
	var nilRecorder *Recorder
	nilRecorder.Once(ctx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)
	require.Empty(t, contextvalues.ConnectionReference(ctx))

	conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
	cfg := testConfig()
	cfg.flushInterval = time.Hour
	recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, cfg)
	require.NoError(t, recorder.Shutdown(ctx))
	recorder.Once(ctx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)
	require.Empty(t, contextvalues.ConnectionReference(ctx), "shutdown dropped the row")
}

func TestReference_NoneForASuccess(t *testing.T) {
	t.Parallel()

	ctx := withSlot(t)
	NewCapturing(func(Row) {}, logsOn).Once(ctx, testScope(), StepMCPRequest, ResultAccepted)
	require.Empty(t, contextvalues.ConnectionReference(ctx))
}

func TestReference_NoneWhileInsertsAreFailing(t *testing.T) {
	t.Parallel()

	conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
	recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, testConfig())
	t.Cleanup(func() { _ = recorder.Shutdown(context.WithoutCancel(t.Context())) })
	recorder.insertFailing.Store(true)

	ctx := withSlot(t)
	recorder.Once(ctx, testScope(), StepMCPRequest, CodeInvalidRemoteSession)
	require.Empty(t, contextvalues.ConnectionReference(ctx))
}

func TestPromiseReference_LeavesTheAttemptForALaterEnd(t *testing.T) {
	t.Parallel()

	attempt := Begin(testScope())
	recorder := NewCapturing(func(Row) {}, logsOn)

	ctx := withSlot(t)
	recorder.PromiseReference(ctx, attempt, CodeExpired)
	require.Equal(t, attempt.ID().String(), contextvalues.ConnectionReference(ctx))

	resultCtx := withSlot(t)
	recorder.PromiseReference(resultCtx, attempt, ResultComplete)
	require.Empty(t, contextvalues.ConnectionReference(resultCtx), "a success gets no reference")
}

// A folded request's reference must be the id its row is written under.
func TestReference_AFoldedRequestShowsTheRowItFoldsInto(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		conn := &idConn{}
		cfg := testConfig()
		cfg.queueSize = 4096
		cfg.repeatsInterval = time.Minute
		recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, cfg)
		scope := testScope()

		var folded []string
		for range onceKeyBurst + 3 {
			ctx := contextvalues.WithConnectionReferenceSlot(t.Context())
			recorder.Once(ctx, scope, StepMCPRequest, CodeInvalidRemoteSession)
			folded = append(folded, contextvalues.ConnectionReference(ctx))
		}
		require.NotEqual(t, folded[onceKeyBurst-1], folded[onceKeyBurst], "the burst's rows keep their own ids")
		require.Equal(t, folded[onceKeyBurst], folded[onceKeyBurst+1], "folded requests share one id")
		require.Equal(t, folded[onceKeyBurst], folded[onceKeyBurst+2])

		time.Sleep(2 * time.Minute) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		synctest.Wait()

		ids := conn.ids()
		require.Contains(t, ids, folded[onceKeyBurst], "the periodic pass wrote the folded row under the id they were shown")
		require.Len(t, ids, onceKeyBurst+1, "folded requests store one row")
		require.NoError(t, recorder.Shutdown(t.Context()))
	})
}

func TestOnceLimiter_TheNextAllowedRowIsStoredUnderTheFoldID(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	first := limiterRow("org", "server", "invalid_request", "")
	var heldID uuid.UUID
	for range onceKeyBurst + 2 {
		row := first
		row.AttemptID = uuid.New()
		if decision := limiter.take(row, now); !decision.allowed {
			if heldID == uuid.Nil {
				heldID = decision.foldID
			}
			require.Equal(t, heldID, decision.foldID, "every held row folds into the first held id")
		}
	}
	require.NotEqual(t, uuid.Nil, heldID)

	next := first
	next.AttemptID = uuid.New()
	decision := limiter.take(next, now.Add(2*onceKeyInterval))
	require.True(t, decision.allowed)
	require.Equal(t, heldID, decision.foldID)

	rows := limiter.pendingRows(now.Add(3*onceKeyInterval), true)
	require.Empty(t, rows, "the fold was written, not held for later")
}

func TestReference_APassOfHeldRowsKeepsTheFoldID(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	row := limiterRow("org", "server", "invalid_request", "")
	row.AttemptID = uuid.New()
	first := limiter.holdBack(row).foldID
	later := row
	later.AttemptID = uuid.New()
	require.Equal(t, first, limiter.holdBack(later).foldID)

	rows := limiter.pendingRows(time.Now(), true)
	require.Len(t, rows, 1)
	require.Equal(t, first, rows[0].AttemptID)
	require.Equal(t, row.AttemptID, first)
}

// idConn records the attempt_id of every stored row.
type idConn struct {
	driver.Conn

	stored atomic.Pointer[[]string]
}

func (c *idConn) ids() []string {
	if v := c.stored.Load(); v != nil {
		return append([]string(nil), *v...)
	}
	return nil
}

func (c *idConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return &idBatch{Batch: nil, conn: c}, nil
}

type idBatch struct {
	driver.Batch

	conn *idConn
}

func (b *idBatch) Append(values ...any) error {
	id, ok := values[2].(uuid.UUID)
	if !ok {
		return errors.New("attempt_id is the third column")
	}
	ids := append(b.conn.ids(), id.String())
	b.conn.stored.Store(&ids)
	return nil
}

func (b *idBatch) Send() error  { return nil }
func (b *idBatch) Abort() error { return nil }
