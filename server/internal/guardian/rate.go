package guardian

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

// ErrRateLimited is the sentinel reason for requests denied because the rate
// limit for their partition is exhausted. It surfaces from HTTP clients built
// with [WithResilience] wrapped in a [ResilienceError] (and, at the call
// site, a [*net/url.Error]), so match it with [errors.Is].
var ErrRateLimited = errors.New("rate limited")

// Limit configures a rate limiter bucket. All fields must be positive:
// limiters reject zero values rather than admit traffic, so a malformed
// config cannot silently disable the guard. To turn rate limiting off, use
// the zero value ([NoLimit]) on a [Policy] instead of passing it to a
// [Limiter].
type Limit struct {
	Rate   uint32
	Burst  uint32
	Period time.Duration
}

// PerSecond returns a Limit that allows rate events per second with a burst
// of the same size.
func PerSecond(rate uint32) Limit {
	return Limit{Rate: rate, Burst: rate, Period: time.Second}
}

// PerMinute returns a Limit that allows rate events per minute with a burst
// of the same size.
func PerMinute(rate uint32) Limit {
	return Limit{Rate: rate, Burst: rate, Period: time.Minute}
}

// PerHour returns a Limit that allows rate events per hour with a burst of
// the same size.
func PerHour(rate uint32) Limit {
	return Limit{Rate: rate, Burst: rate, Period: time.Hour}
}

type RateLimitResult struct {
	// Limit is the limit that was used to obtain this result.
	Limit Limit

	// Allowed is the number of events that may happen at time now.
	Allowed int

	// Remaining is the maximum number of requests that could be
	// permitted instantaneously for this key given the current
	// state. For example, if a rate limiter allows 10 requests per
	// second and has already received 6 requests for this key this
	// second, Remaining would be 4.
	Remaining int

	// RetryAfter is the time until the next request will be permitted.
	// It should be -1 unless the rate limit has been exceeded.
	RetryAfter time.Duration

	// ResetAfter is the time until the RateLimiter returns to its
	// initial state for a given key. For example, if a rate limiter
	// manages requests per second and received one request 200ms ago,
	// Reset would return 800ms. You can also think of this as the time
	// until Limit and Remaining will be equal.
	ResetAfter time.Duration
}

// Wait blocks until RetryAfter has elapsed or the context is done. It returns
// nil immediately if the result did not exceed the rate limit. Waiting does
// not guarantee that a retry will succeed since concurrent callers may
// consume the freed capacity first - retry AllowN in a loop.
func (r RateLimitResult) Wait(ctx context.Context) error {
	if r.RetryAfter <= 0 {
		return nil
	}

	if r.RetryAfter >= rate.InfDuration {
		return errors.New("wait for rate limit: request exceeds burst size and will never be permitted")
	}

	if deadline, ok := ctx.Deadline(); ok && deadline.Before(time.Now().Add(r.RetryAfter)) {
		return fmt.Errorf("wait for rate limit: %w", context.DeadlineExceeded)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for rate limit: %w", ctx.Err())
	case <-time.After(r.RetryAfter):
		return nil
	}
}

type Limiter interface {
	AllowN(
		ctx context.Context,
		key Partition,
		limit Limit,
		n uint32,
	) (RateLimitResult, error)
}
