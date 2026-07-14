package guardian

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestRateLimitResult_Wait_Allowed(t *testing.T) {
	t.Parallel()

	res := RateLimitResult{
		Limit:      PerSecond(10),
		Allowed:    1,
		Remaining:  9,
		RetryAfter: -1,
		ResetAfter: 100 * time.Millisecond,
	}

	require.NoError(t, res.Wait(t.Context()))
}

func TestRateLimitResult_Wait_NeverPermitted(t *testing.T) {
	t.Parallel()

	res := RateLimitResult{
		Limit:      PerSecond(10),
		Allowed:    0,
		Remaining:  10,
		RetryAfter: rate.InfDuration,
		ResetAfter: 0,
	}

	err := res.Wait(t.Context())
	require.ErrorContains(t, err, "never be permitted")
}

func TestRateLimitResult_Wait_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	res := RateLimitResult{
		Limit:      PerSecond(10),
		Allowed:    0,
		Remaining:  0,
		RetryAfter: time.Minute,
		ResetAfter: time.Minute,
	}

	err := res.Wait(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRateLimitResult_Wait_DeadlineTooShort(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		res := RateLimitResult{
			Limit:      PerSecond(10),
			Allowed:    0,
			Remaining:  0,
			RetryAfter: time.Minute,
			ResetAfter: time.Minute,
		}

		start := time.Now()
		err := res.Wait(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Zero(t, time.Since(start), "should fail fast instead of waiting out the deadline")
	})
}

func TestRateLimitResult_Wait_Elapses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		res := RateLimitResult{
			Limit:      PerSecond(10),
			Allowed:    0,
			Remaining:  0,
			RetryAfter: 100 * time.Millisecond,
			ResetAfter: time.Second,
		}

		start := time.Now()
		require.NoError(t, res.Wait(t.Context()))
		require.Equal(t, 100*time.Millisecond, time.Since(start))
	})
}
