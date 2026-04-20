package background

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestThrottle_LeadingEdge(t *testing.T) {
	t.Parallel()

	var trailingCount atomic.Int64
	throttle := NewThrottle[string, string](
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
	throttle := NewThrottle[string, string](
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
	throttle := NewThrottle[int, int](
		50*time.Millisecond,
		func(v int) int { return v },
		func(v int) error { trailingCount.Add(1); return nil },
	)

	throttle.Do(42) // leading fire, no subsequent calls

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(0), trailingCount.Load(), "no trailing when nothing was pending")
}

func TestThrottle_ResetsAfterCooldown(t *testing.T) {
	t.Parallel()

	throttle := NewThrottle[string, string](
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
