package activities

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

type scriptedLimiter struct {
	results []ratelimit.Result
	errs    []error
	keys    []string
}

func (s *scriptedLimiter) Allow(_ context.Context, key string) (ratelimit.Result, error) {
	s.keys = append(s.keys, key)
	idx := len(s.keys) - 1
	return s.results[idx], s.errs[idx]
}

func TestWorkOSRateLimiterWaitRetriesUntilAllowed(t *testing.T) {
	t.Parallel()

	limiter := &scriptedLimiter{
		results: []ratelimit.Result{
			{Allowed: false, Remaining: 0, RetryAfter: time.Nanosecond},
			{Allowed: true, Remaining: 1, RetryAfter: 0},
		},
		errs: []error{nil, nil},
		keys: nil,
	}

	err := (&redisWorkOSRateLimiter{limiter: limiter}).Wait(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{workosAPIRateLimitKey, workosAPIRateLimitKey}, limiter.keys)
}

func TestWorkOSRateLimiterWaitStopsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	limiter := &scriptedLimiter{
		results: []ratelimit.Result{{Allowed: false, Remaining: 0, RetryAfter: time.Hour}},
		errs:    []error{nil},
		keys:    nil,
	}

	err := (&redisWorkOSRateLimiter{limiter: limiter}).Wait(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWorkOSRateLimiterWaitReturnsStoreError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("redis unavailable")
	limiter := &scriptedLimiter{
		results: []ratelimit.Result{{Allowed: false, Remaining: 0, RetryAfter: 0}},
		errs:    []error{wantErr},
		keys:    nil,
	}

	err := (&redisWorkOSRateLimiter{limiter: limiter}).Wait(t.Context())
	require.ErrorIs(t, err, wantErr)
}
