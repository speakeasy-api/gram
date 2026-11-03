package mcpconnectionattempts

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func limiterAllows(limiter *onceLimiter, row Row, now time.Time) bool {
	return limiter.take(row, now).allowed
}

func limiterRow(organizationID, serverID, reason, subject string) Row {
	var row Row
	row.OrganizationID, row.ServerID, row.Step, row.Reason, row.SubjectURN = organizationID, serverID, StepInboundAuthorize.String(), reason, subject
	return row
}

func TestOnceLimiter_RepeatingKeyGetsABurstThenAboutOneAMinute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newOnceLimiter()
		row := limiterRow("org", "server", "invalid_request", "")
		for range onceKeyBurst {
			require.True(t, limiterAllows(limiter, row, time.Now()))
		}
		require.False(t, limiterAllows(limiter, row, time.Now()))

		time.Sleep(onceKeyInterval) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		require.True(t, limiterAllows(limiter, row, time.Now()))
		require.False(t, limiterAllows(limiter, row, time.Now()))
	})
}

func TestOnceLimiter_KeysAreSeparate(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	row := limiterRow("org", "server", "invalid_request", "user:a")
	for range onceKeyBurst {
		require.True(t, limiterAllows(limiter, row, now))
	}
	require.False(t, limiterAllows(limiter, row, now))

	require.True(t, limiterAllows(limiter, limiterRow("other-org", "server", "invalid_request", "user:a"), now))
	require.True(t, limiterAllows(limiter, limiterRow("org", "other-server", "invalid_request", "user:a"), now))
	require.True(t, limiterAllows(limiter, limiterRow("org", "server", "expired", "user:a"), now))
	require.True(t, limiterAllows(limiter, limiterRow("org", "server", "invalid_request", "user:b"), now))
	otherProvider := row
	otherProvider.ProviderHost = "login.example.com"
	require.True(t, limiterAllows(limiter, otherProvider, now), "one provider can't hold back another")
	otherMember := row
	otherMember.MemberID = "member"
	require.True(t, limiterAllows(limiter, otherMember, now), "one gateway member can't hold back another")
	otherStep := row
	otherStep.Step = StepInboundToken.String()
	require.True(t, limiterAllows(limiter, otherStep, now), "one stage can't hold back another")
}

// A public server gives every request a new anonymous subject, which must not earn a new key.
func TestOnceLimiter_AnonymousSubjectsShareAKey(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	allowed := 0
	for range onceKeyBurst * 3 {
		if limiterAllows(limiter, limiterRow("org", "server", "invalid_request", "anonymous:"+uuid.NewString()), now) {
			allowed++
		}
	}
	require.Equal(t, onceKeyBurst, allowed)
}

// Varied codes, servers and subjects can't escape the organization's ceiling.
func TestOnceLimiter_OrganizationCeilingAcrossKeys(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	allowed := 0
	for i := range onceOrgBurst * 3 {
		label := strconv.Itoa(i)
		if limiterAllows(limiter, limiterRow("org", "server-"+label, "code-"+label, "user:"+label), now) {
			allowed++
		}
	}
	require.Equal(t, onceOrgBurst, allowed)
	require.True(t, limiterAllows(limiter, limiterRow("other-org", "server", "invalid_request", ""), now), "one organization's flood leaves others alone")
}

// A noisy key can't use up the ceiling, so other rows in the organization still get through.
func TestOnceLimiter_AHeldBackKeySpendsNoCeiling(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	noisy := limiterRow("org", "server", "invalid_request", "")
	for range onceOrgBurst * 3 {
		limiterAllows(limiter, noisy, now)
	}

	allowed := 0
	for i := range onceOrgBurst * 2 {
		if limiterAllows(limiter, limiterRow("org", strconv.Itoa(i), "expired", ""), now) {
			allowed++
		}
	}
	require.Equal(t, onceOrgBurst-onceKeyBurst, allowed)
}

// A row the ceiling refused wasn't recorded, so its key keeps the token for later.
func TestOnceLimiter_ACeilingRefusalSpendsNoKeyToken(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	for i := range onceOrgBurst {
		require.True(t, limiterAllows(limiter, limiterRow("org", strconv.Itoa(i), "invalid_request", ""), now))
	}
	row := limiterRow("org", "quiet", "invalid_request", "")
	for range onceKeyBurst * 2 {
		require.False(t, limiterAllows(limiter, row, now))
	}

	later := now.Add(time.Second)
	for range onceKeyBurst {
		require.True(t, limiterAllows(limiter, row, later))
	}
	require.False(t, limiterAllows(limiter, row, later), "the key's own burst is still the limit")
}

func TestOnceLimiter_MemoryStaysBounded(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	for i := range onceMaxKeys + 1000 {
		limiterAllows(limiter, limiterRow("org-"+strconv.Itoa(i), "server", "invalid_request", ""), now)
	}
	require.Equal(t, onceMaxKeys, limiter.keys.Len())
	require.Equal(t, onceMaxOrgs, limiter.orgs.Len())
}

// Real attempts have proof of a flow, so a flood of Once rows can't hold them back.
func TestRecord_RealAttemptsAreUnaffectedByAOnceFlood(t *testing.T) {
	t.Parallel()

	conn := &slowConn{Conn: nil, delay: 0, sizes: nil}
	cfg := testConfig()
	cfg.queueSize = 4096
	recorder := newTestRecorder(t, conn, cfg)
	scope := testScope()

	const flood = 1000
	for range flood {
		recorder.Once(t.Context(), scope, StepInboundAuthorize, CodeInvalidRequest)
	}
	attempt := Begin(scope)
	const steps = 50
	for range steps {
		recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	}
	recorder.End(t.Context(), attempt, StepInboundConsent, CodeCanceled)
	require.NoError(t, recorder.Shutdown(t.Context()))

	written := 0
	for _, size := range conn.sizes {
		written += size
	}
	require.Equal(t, onceKeyBurst+1+steps+1, written, "the burst, one row for the held-back count, and every real step")
	require.Equal(t, int64(flood-onceKeyBurst), recorder.dropped(t, dropRateLimited))
}

// The check happens before the queue, without I/O, so a stalled writer can't hold it up.
func TestOnce_HeldBackRowsNeverBlockOrReachTheQueue(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	defer close(release)
	conn := &scriptedConn{Conn: nil, block: release, err: nil, prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.queueSize = onceKeyBurst * 2
	cfg.insertTimeout = time.Minute
	recorder := newTestRecorder(t, conn, cfg)
	scope := testScope()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range 1000 {
			recorder.Once(t.Context(), scope, StepInboundAuthorize, CodeInvalidRequest)
		}
	}()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Once blocked")
	}
	require.Equal(t, int64(1000-onceKeyBurst), recorder.dropped(t, dropRateLimited))
	require.Zero(t, recorder.dropped(t, dropQueueFull))
}

func TestOnce_AHeldBackRowCountsNoStep(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}
	recorder := newTestRecorder(t, conn, testConfig())
	scope := testScope()
	for range onceKeyBurst + 2 {
		recorder.Once(t.Context(), scope, StepInboundAuthorize, CodeInvalidRequest)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(onceKeyBurst+1), stepsCounted(t, recorder), "the burst and the row standing for the rest")
	require.Equal(t, int64(2), recorder.dropped(t, dropRateLimited))
}

// A burst across keys stops at its share of the queue, leaving room for real attempts.
func TestOnce_LeavesTheQueueReserveForRealAttempts(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	defer close(release)
	conn := &scriptedConn{Conn: nil, block: release, err: errors.New("stalled"), prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.queueSize = 8
	cfg.maxBatch = 1
	cfg.insertTimeout = time.Minute
	recorder := newTestRecorder(t, conn, cfg)
	attempt := Begin(testScope())

	recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	require.Eventually(t, func() bool { return conn.prepares.Load() == 1 }, 10*time.Second, time.Millisecond)

	for range 20 {
		recorder.Once(t.Context(), testScope(), StepInboundAuthorize, CodeInvalidRequest)
	}
	require.Equal(t, int64(14), recorder.dropped(t, dropQueueReserve), "6 of 8 slots go to Once rows")
	require.Zero(t, recorder.dropped(t, dropRateLimited), "a backed-up queue doesn't read as a noisy key")

	recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	recorder.End(t.Context(), attempt, StepInboundConsent, CodeCanceled)
	require.Zero(t, recorder.dropped(t, dropQueueFull), "real attempts took the reserve")

	recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	require.Equal(t, int64(1), recorder.dropped(t, dropQueueFull), "real attempts aren't held back, only limited by the queue")
}

// repeatsConn keeps each row's attempt count: one for the row, plus what it stands for.
type repeatsConn struct {
	driver.Conn

	rows    atomic.Int64
	repeats atomic.Int64
}

func (c *repeatsConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return &repeatsBatch{Batch: nil, conn: c}, nil
}

type repeatsBatch struct {
	driver.Batch

	conn *repeatsConn
}

func (b *repeatsBatch) Append(values ...any) error {
	b.conn.rows.Add(1)
	repeats, ok := values[len(values)-2].(uint32)
	if !ok {
		return errors.New("repeats is the second last column")
	}
	b.conn.repeats.Add(int64(repeats))
	return nil
}

func (b *repeatsBatch) Send() error { return nil }

func (b *repeatsBatch) Abort() error { return nil }

// A burst that stops still reports: its rows plus their repeats add up to the true count.
func TestRepeats_ABurstThenSilenceAddsUp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
		cfg := testConfig()
		cfg.queueSize = 4096
		cfg.repeatsInterval = time.Minute
		recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, cfg)
		scope := testScope()

		for range 100 {
			recorder.Once(t.Context(), scope, StepInboundAuthorize, CodeInvalidRequest)
		}
		time.Sleep(2 * time.Minute) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		synctest.Wait()

		require.Equal(t, int64(onceKeyBurst+1), conn.rows.Load(), "no shutdown yet: the periodic pass reported the burst")
		require.Equal(t, int64(100), conn.rows.Load()+conn.repeats.Load())
		require.NoError(t, recorder.Shutdown(t.Context()))
		require.Equal(t, int64(100), conn.rows.Load()+conn.repeats.Load(), "nothing is counted twice")
	})
}

func TestRepeats_ShutdownFlushes(t *testing.T) {
	t.Parallel()

	conn := &repeatsConn{Conn: nil, rows: atomic.Int64{}, repeats: atomic.Int64{}}
	cfg := testConfig()
	cfg.repeatsInterval = time.Hour
	recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, cfg)
	scope := testScope()
	for range 40 {
		recorder.Once(t.Context(), scope, StepInboundAuthorize, CodeInvalidRequest)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(40), conn.rows.Load()+conn.repeats.Load())
	require.Zero(t, recorder.unfinished.Load())
}

func TestRepeats_TheNextAllowedRowCarriesTheCountThenZero(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	row := limiterRow("org", "server", "invalid_request", "")
	for range onceKeyBurst {
		require.Zero(t, limiter.take(row, now).repeats)
	}
	for range 7 {
		require.False(t, limiter.take(row, now).allowed)
	}

	later := now.Add(onceKeyInterval)
	decision := limiter.take(row, later)
	require.True(t, decision.allowed)
	require.Equal(t, uint32(7), decision.repeats)
	require.Empty(t, limiter.pendingRows(later, false), "the count went on that row")
	require.Zero(t, limiter.take(row, later.Add(onceKeyInterval)).repeats)
}

func TestRepeats_OrganizationCeilingCountsOnTheKey(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	for i := range onceOrgBurst {
		require.True(t, limiterAllows(limiter, limiterRow("org", strconv.Itoa(i), "invalid_request", ""), now))
	}
	row := limiterRow("org", "quiet", "invalid_request", "")
	require.False(t, limiterAllows(limiter, row, now))
	require.False(t, limiterAllows(limiter, row, now))

	later := now.Add(time.Second)
	decision := limiter.take(row, later)
	require.True(t, decision.allowed)
	require.Equal(t, uint32(2), decision.repeats)
}

func TestRepeats_QueueReservedRowsCountOnTheKey(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	row := limiterRow("org", "server", "invalid_request", "")
	require.Zero(t, limiter.holdBack(row))
	require.Zero(t, limiter.holdBack(row))

	rows := limiter.pendingRows(time.Now(), false)
	require.Len(t, rows, 1)
	require.Equal(t, uint32(1), rows[0].Repeats, "the row stands for itself and one more")
}

// A periodic pass respects the ceiling, and the last one at shutdown doesn't lose the count.
func TestRepeats_PeriodicPassStaysWithinTheCeiling(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	for i := range onceOrgBurst {
		require.True(t, limiterAllows(limiter, limiterRow("org", strconv.Itoa(i), "invalid_request", ""), now))
	}
	require.Zero(t, limiter.holdBack(limiterRow("org", "quiet", "invalid_request", "")))

	require.Empty(t, limiter.pendingRows(now, false))
	require.Len(t, limiter.pendingRows(now, true), 1)
}

func TestRepeats_EvictionCountsWhatItLoses(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	first := limiterRow("org-first", "server", "invalid_request", "")
	for range 3 {
		require.Zero(t, limiter.holdBack(first))
	}

	lost := 0
	now := time.Now()
	for i := range onceMaxKeys {
		lost += limiter.take(limiterRow("org-"+strconv.Itoa(i), "server", "invalid_request", ""), now).lost
	}
	require.Equal(t, 3, lost)
	require.Empty(t, limiter.pendingRows(now, true))
	require.LessOrEqual(t, len(limiter.withPending), onceMaxKeys)
}

func TestRecord_EvictedRepeatsCountInTheDropMetric(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: nil, err: errors.New("refused"), prepares: atomic.Int64{}}
	cfg := testConfig()
	recorder := newTestRecorder(t, conn, cfg)
	recorder.limiter.lost = 4

	recorder.Once(t.Context(), testScope(), StepInboundAuthorize, CodeInvalidRequest)

	require.Equal(t, int64(4), recorder.dropped(t, dropRepeatsLost))
}
