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
	calls []uuid.UUID
	count atomic.Int64
	err   error
}

func (s *countingSignaler) Signal(_ context.Context, projectID uuid.UUID) error {
	s.count.Add(1)
	s.mu.Lock()
	s.calls = append(s.calls, projectID)
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

	err := throttled.Signal(t.Context(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, inner.callCount(), "first call should fire immediately")
}

func TestThrottledSignaler_CoalescesDuringCooldown(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	projectID := uuid.New()

	_ = throttled.Signal(t.Context(), projectID)
	assert.Equal(t, 1, inner.callCount())

	for range 50 {
		_ = throttled.Signal(t.Context(), projectID)
	}

	assert.Equal(t, 1, inner.callCount(), "calls during cooldown should be suppressed")

	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, 2, inner.callCount(), "exactly one trailing signal should fire after cooldown")
}

func TestThrottledSignaler_NoPendingNoTrailing(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 50*time.Millisecond, testenv.NewLogger(t))

	_ = throttled.Signal(t.Context(), uuid.New())
	assert.Equal(t, 1, inner.callCount())

	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 1, inner.callCount(), "no trailing signal when nothing was pending")
}

func TestThrottledSignaler_IndependentPerProject(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	_ = throttled.Signal(t.Context(), uuid.New())
	_ = throttled.Signal(t.Context(), uuid.New())

	assert.Equal(t, 2, inner.callCount(), "different projects should throttle independently")
}

func TestThrottledSignaler_ZeroCooldownDisablesThrottling(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 0, testenv.NewLogger(t))

	projectID := uuid.New()
	for range 5 {
		_ = throttled.Signal(t.Context(), projectID)
	}

	assert.Equal(t, 5, inner.callCount(), "zero cooldown should pass through all calls")
}

func TestThrottledSignaler_RecoversAfterCooldown(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 50*time.Millisecond, testenv.NewLogger(t))

	projectID := uuid.New()

	_ = throttled.Signal(t.Context(), projectID)
	assert.Equal(t, 1, inner.callCount())

	time.Sleep(100 * time.Millisecond)

	_ = throttled.Signal(t.Context(), projectID)
	assert.Equal(t, 2, inner.callCount(), "should fire immediately after cooldown expires")
}

func TestThrottledSignaler_ConcurrentCallers(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	projectID := uuid.New()

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			_ = throttled.Signal(t.Context(), projectID)
		})
	}
	wg.Wait()

	assert.Equal(t, 1, inner.callCount(), "concurrent callers should result in exactly one immediate signal")

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 2, inner.callCount(), "one trailing signal after concurrent burst")
}

func TestThrottledSignaler_MultipleBursts(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 50*time.Millisecond, testenv.NewLogger(t))

	projectID := uuid.New()

	_ = throttled.Signal(t.Context(), projectID)
	_ = throttled.Signal(t.Context(), projectID)
	assert.Equal(t, 1, inner.callCount())

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 2, inner.callCount(), "trailing from burst 1")

	time.Sleep(100 * time.Millisecond)

	_ = throttled.Signal(t.Context(), projectID)
	_ = throttled.Signal(t.Context(), projectID)
	assert.Equal(t, 3, inner.callCount(), "immediate from burst 2")

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 4, inner.callCount(), "trailing from burst 2")
}

func TestThrottledSignaler_FirstCallErrorPropagates(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{err: assert.AnError}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	err := throttled.Signal(t.Context(), uuid.New())
	require.ErrorIs(t, err, assert.AnError, "first call error should propagate")
}

func TestThrottledSignaler_SuppressedCallsReturnNil(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, 100*time.Millisecond, testenv.NewLogger(t))

	projectID := uuid.New()
	_ = throttled.Signal(t.Context(), projectID)

	err := throttled.Signal(t.Context(), projectID)
	require.NoError(t, err, "suppressed calls should return nil")
}

func TestThrottledSignaler_NegativeCooldownDisablesThrottling(t *testing.T) {
	t.Parallel()

	inner := &countingSignaler{}
	throttled := NewThrottledSignaler(inner, -1*time.Second, testenv.NewLogger(t))

	projectID := uuid.New()
	for range 5 {
		_ = throttled.Signal(t.Context(), projectID)
	}

	assert.Equal(t, 5, inner.callCount(), "negative cooldown should pass through all calls")
}
