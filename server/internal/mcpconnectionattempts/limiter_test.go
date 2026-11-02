package mcpconnectionattempts

import (
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func limiterRow(organizationID, serverID, reason, subject string) Row {
	var row Row
	row.OrganizationID, row.ServerID, row.Reason, row.SubjectURN = organizationID, serverID, reason, subject
	return row
}

func TestOnceLimiter_RepeatingKeyGetsABurstThenAboutOneAMinute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newOnceLimiter()
		row := limiterRow("org", "server", "invalid_request", "")
		for range onceKeyBurst {
			require.True(t, limiter.allow(row, time.Now()))
		}
		require.False(t, limiter.allow(row, time.Now()))

		time.Sleep(onceKeyInterval) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		require.True(t, limiter.allow(row, time.Now()))
		require.False(t, limiter.allow(row, time.Now()))
	})
}

func TestOnceLimiter_KeysAreSeparate(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	row := limiterRow("org", "server", "invalid_request", "user:a")
	for range onceKeyBurst {
		require.True(t, limiter.allow(row, now))
	}
	require.False(t, limiter.allow(row, now))

	require.True(t, limiter.allow(limiterRow("other-org", "server", "invalid_request", "user:a"), now))
	require.True(t, limiter.allow(limiterRow("org", "other-server", "invalid_request", "user:a"), now))
	require.True(t, limiter.allow(limiterRow("org", "server", "expired", "user:a"), now))
	require.True(t, limiter.allow(limiterRow("org", "server", "invalid_request", "user:b"), now))
}

// A public server gives every request a new anonymous subject, which must not earn a new key.
func TestOnceLimiter_AnonymousSubjectsShareAKey(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	allowed := 0
	for range onceKeyBurst * 3 {
		if limiter.allow(limiterRow("org", "server", "invalid_request", "anonymous:"+uuid.NewString()), now) {
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
		if limiter.allow(limiterRow("org", strconv.Itoa(i), "invalid_request", ""), now) {
			allowed++
		}
	}
	require.Equal(t, onceOrgBurst, allowed)
	require.True(t, limiter.allow(limiterRow("other-org", "server", "invalid_request", ""), now), "one organization's flood leaves others alone")
}

func TestOnceLimiter_MemoryStaysBounded(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	for i := range onceMaxKeys + 1000 {
		limiter.allow(limiterRow("org-"+strconv.Itoa(i), "server", "invalid_request", ""), now)
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
	require.Equal(t, onceKeyBurst+steps+1, written)
	require.Equal(t, int64(flood-onceKeyBurst), recorder.dropped(t, dropRateLimited))
}

// The check happens before the queue, without I/O, so a stalled writer can't hold it up.
func TestOnce_HeldBackRowsNeverBlockOrReachTheQueue(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	defer close(release)
	conn := &scriptedConn{Conn: nil, block: release, err: nil, prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.queueSize = onceKeyBurst
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

	require.Equal(t, int64(onceKeyBurst), stepsCounted(t, recorder))
	require.Equal(t, int64(2), recorder.dropped(t, dropRateLimited))
}
