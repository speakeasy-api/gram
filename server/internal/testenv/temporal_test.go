package testenv

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
)

// errThrottled is the error the dev server returns when it throttles a
// request. Tests return it wrapped, as NewTemporalEnvironment does.
var errThrottled = serviceerror.NewResourceExhausted(enumspb.RESOURCE_EXHAUSTED_CAUSE_SYSTEM_OVERLOADED, "service rate limit exceeded")

func TestRetryThrottledRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		err := retryThrottled(t.Context(), func(context.Context) error {
			calls++
			if calls < 3 {
				return fmt.Errorf("register: %w", errThrottled)
			}
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 3, calls)
	})
}

func TestRetryThrottledReturnsOtherErrorsAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		errOther := serviceerror.NewNamespaceAlreadyExists("namespace already exists")
		calls := 0
		err := retryThrottled(t.Context(), func(context.Context) error {
			calls++
			return fmt.Errorf("register: %w", errOther)
		})

		require.ErrorIs(t, err, errOther)
		require.Equal(t, 1, calls)
	})
}

func TestRetryThrottledGivesUpWithBackoff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		start := time.Now()
		err := retryThrottled(t.Context(), func(context.Context) error {
			calls++
			return fmt.Errorf("register: %w", errThrottled)
		})
		elapsed := time.Since(start)

		var throttled *serviceerror.ResourceExhausted
		require.ErrorAs(t, err, &throttled)
		require.ErrorContains(t, err, "still throttled after 10 attempts")
		require.Equal(t, throttledAttempts, calls)

		// Nine waits between ten attempts: 100ms doubling to the 2s cap
		// gives 11.1s in full, and each wait is jittered to between half and
		// all of its length.
		require.GreaterOrEqual(t, elapsed, 5550*time.Millisecond)
		require.Less(t, elapsed, 11100*time.Millisecond)
	})
}

func TestRetryThrottledStopsWhenContextEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		err := retryThrottled(ctx, func(context.Context) error {
			calls++
			cancel()
			return fmt.Errorf("register: %w", errThrottled)
		})

		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, err, errThrottled)
		require.Equal(t, 1, calls)
	})
}
