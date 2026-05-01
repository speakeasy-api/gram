package background

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type countingSignaler struct {
	mu    sync.Mutex
	calls []DrainRiskAnalysisParams
	count atomic.Int64
	err   error
}

func (s *countingSignaler) SignalNewMessages(_ context.Context, params DrainRiskAnalysisParams) error {
	s.count.Add(1)
	s.mu.Lock()
	s.calls = append(s.calls, params)
	s.mu.Unlock()
	return s.err
}

func (s *countingSignaler) callCount() int {
	return int(s.count.Load())
}

func TestThrottledSignaler_FirstCallFiresImmediately(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	err := throttled.SignalNewMessages(context.Background(), params)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.callCount(), "first call should fire immediately")
}

func TestThrottledSignaler_CoalescesDuringCooldown(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	// First call fires immediately.
	_ = throttled.SignalNewMessages(context.Background(), params)
	assert.Equal(t, 1, inner.callCount())

	// Rapid-fire 50 more calls within the cooldown window.
	for range 50 {
		_ = throttled.SignalNewMessages(context.Background(), params)
	}

	// Should still be 1 — all suppressed.
	assert.Equal(t, 1, inner.callCount(), "calls during cooldown should be suppressed")

	// Wait for the cooldown to expire and the trailing signal to fire.
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, 2, inner.callCount(), "exactly one trailing signal should fire after cooldown")
}

func TestThrottledSignaler_NoPendingNoTrailing(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 50*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	// Single call, then wait for cooldown.
	_ = throttled.SignalNewMessages(context.Background(), params)
	assert.Equal(t, 1, inner.callCount())

	time.Sleep(100 * time.Millisecond)

	// No trailing signal because nothing was pending.
	assert.Equal(t, 1, inner.callCount(), "no trailing signal when nothing was pending")
}

func TestThrottledSignaler_IndependentPerPolicy(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	policy1 := DrainRiskAnalysisParams{ProjectID: uuid.New(), RiskPolicyID: uuid.New()}
	policy2 := DrainRiskAnalysisParams{ProjectID: uuid.New(), RiskPolicyID: uuid.New()}

	// Both should fire immediately — different policies.
	_ = throttled.SignalNewMessages(context.Background(), policy1)
	_ = throttled.SignalNewMessages(context.Background(), policy2)

	assert.Equal(t, 2, inner.callCount(), "different policies should throttle independently")
}

func TestThrottledSignaler_ZeroCooldownDisablesThrottling(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 0, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	for range 5 {
		_ = throttled.SignalNewMessages(context.Background(), params)
	}

	assert.Equal(t, 5, inner.callCount(), "zero cooldown should pass through all calls")
}

func TestThrottledSignaler_RecoversAfterCooldown(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 50*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	// First burst.
	_ = throttled.SignalNewMessages(context.Background(), params)
	assert.Equal(t, 1, inner.callCount())

	// Wait for cooldown to fully expire.
	time.Sleep(100 * time.Millisecond)

	// Second call should fire immediately again.
	_ = throttled.SignalNewMessages(context.Background(), params)
	assert.Equal(t, 2, inner.callCount(), "should fire immediately after cooldown expires")
}

func TestThrottledSignaler_ConcurrentCallers(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	// Launch 100 concurrent callers.
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			_ = throttled.SignalNewMessages(context.Background(), params)
		})
	}
	wg.Wait()

	// Exactly 1 immediate signal.
	assert.Equal(t, 1, inner.callCount(), "concurrent callers should result in exactly one immediate signal")

	// Wait for trailing.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 2, inner.callCount(), "one trailing signal after concurrent burst")
}

func TestThrottledSignaler_MultipleBursts(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 50*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	// Burst 1: immediate + suppressed.
	_ = throttled.SignalNewMessages(context.Background(), params)
	_ = throttled.SignalNewMessages(context.Background(), params)
	assert.Equal(t, 1, inner.callCount())

	// Wait for trailing from burst 1.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 2, inner.callCount(), "trailing from burst 1")

	// Wait for cooldown to fully expire.
	time.Sleep(100 * time.Millisecond)

	// Burst 2: should fire immediately again + trailing.
	_ = throttled.SignalNewMessages(context.Background(), params)
	_ = throttled.SignalNewMessages(context.Background(), params)
	assert.Equal(t, 3, inner.callCount(), "immediate from burst 2")

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 4, inner.callCount(), "trailing from burst 2")
}

func TestThrottledSignaler_FirstCallErrorPropagates(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{err: assert.AnError}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	err := throttled.SignalNewMessages(context.Background(), params)
	require.ErrorIs(t, err, assert.AnError, "first call error should propagate")
}

func TestThrottledSignaler_SuppressedCallsReturnNil(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	_ = throttled.SignalNewMessages(context.Background(), params)

	// Suppressed calls should return nil, not an error.
	err := throttled.SignalNewMessages(context.Background(), params)
	require.NoError(t, err, "suppressed calls should return nil")
}

func TestThrottledSignaler_NegativeCooldownDisablesThrottling(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, -1*time.Second, testenv.NewLogger(t))

	params := DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	}

	for range 5 {
		_ = throttled.SignalNewMessages(context.Background(), params)
	}

	assert.Equal(t, 5, inner.callCount(), "negative cooldown should pass through all calls")
}
