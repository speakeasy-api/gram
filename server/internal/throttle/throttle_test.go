package throttle

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThrottle_LeadingEdge(t *testing.T) {
	t.Parallel()

	var trailingCount atomic.Int64
	throttle := New[string, string](
		100*time.Millisecond,
		func(v string) string { return v },
		func(v string) error { trailingCount.Add(1); return nil },
	)

	assert.True(t, throttle.Do("a"), "first call should fire")
	assert.False(t, throttle.Do("a"), "second call within window should be suppressed")
	assert.True(t, throttle.Do("b"), "different key should fire independently")
}

func TestThrottle_TrailingFire(t *testing.T) {
	t.Parallel()

	var trailingCount atomic.Int64
	throttle := New[string, string](
		50*time.Millisecond,
		func(v string) string { return v },
		func(v string) error { trailingCount.Add(1); return nil },
	)

	throttle.Do("x")
	throttle.Do("x") // suppressed, marks pending

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(1), trailingCount.Load(), "trailing callback should fire once")
}

func TestThrottle_NoTrailingWithoutPending(t *testing.T) {
	t.Parallel()

	var trailingCount atomic.Int64
	throttle := New[int, int](
		50*time.Millisecond,
		func(v int) int { return v },
		func(v int) error { trailingCount.Add(1); return nil },
	)

	throttle.Do(42) // leading fire, no subsequent calls

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(0), trailingCount.Load(), "no trailing when nothing was pending")
}

func TestThrottle_FlushFiresPending(t *testing.T) {
	t.Parallel()

	var fired atomic.Int64
	var lastValue atomic.Value
	th := New[string, string](
		time.Hour, // long cooldown so timer never fires naturally
		func(v string) string { return v },
		func(v string) error { fired.Add(1); lastValue.Store(v); return nil },
	)

	th.Do("a")                   // leading edge
	th.Do("a")                   // suppressed, pending=true, latest="a"
	require.False(t, th.Do("a")) // still suppressed

	th.Flush()

	require.Equal(t, int64(1), fired.Load(), "flush should fire the pending trailing callback")
	require.Equal(t, "a", lastValue.Load().(string))

	// After flush, the throttle should be clean and accept new leading calls.
	require.True(t, th.Do("a"), "should fire again after flush")
}

func TestThrottle_FlushNoopWhenNoPending(t *testing.T) {
	t.Parallel()

	var fired atomic.Int64
	th := New[string, string](
		time.Hour,
		func(v string) string { return v },
		func(v string) error { fired.Add(1); return nil },
	)

	// Leading fire only, no suppressed calls.
	th.Do("a")
	th.Flush()

	require.Equal(t, int64(0), fired.Load(), "flush should not fire when nothing is pending")

	// Throttle should be clean.
	require.True(t, th.Do("a"), "should fire again after flush")
}

func TestThrottle_FlushMultipleKeys(t *testing.T) {
	t.Parallel()

	var fired atomic.Int64
	th := New[string, string](
		time.Hour,
		func(v string) string { return v },
		func(v string) error { fired.Add(1); return nil },
	)

	th.Do("a")
	th.Do("a") // pending
	th.Do("b")
	th.Do("b") // pending
	th.Do("c") // leading only, no pending

	th.Flush()

	require.Equal(t, int64(2), fired.Load(), "flush should fire trailing for both pending keys")
}

func TestThrottle_ResetsAfterCooldown(t *testing.T) {
	t.Parallel()

	throttle := New[string, string](
		50*time.Millisecond,
		func(v string) string { return v },
		func(v string) error { return nil },
	)

	assert.True(t, throttle.Do("k"))
	assert.False(t, throttle.Do("k")) // marks pending → trailing fires at 50ms, resets timer

	// Wait for both the trailing fire (50ms) and its cooldown (another 50ms).
	time.Sleep(150 * time.Millisecond)

	assert.True(t, throttle.Do("k"), "should fire again after cooldown expires")
}
