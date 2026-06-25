// Package ratelimit is Gram's shared token-bucket rate limiter. A Limiter binds
// one named Rate and enforces it across many keys (per-org, per-assistant, …)
// through a Redis Store, so the cap holds fleet-wide instead of per-replica —
// the limitation of the hand-rolled in-memory limiters this package replaces.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// keyPrefix namespaces every bucket so rate-limit state can never collide with
// other keys sharing the same Redis.
const keyPrefix = "ratelimit:"

// Rate is a token-bucket configuration: Tokens are refilled smoothly over
// Interval, and the bucket holds at most Burst tokens. Sustained throughput is
// Tokens/Interval; Burst caps how many tokens a momentarily idle key may spend
// at once.
type Rate struct {
	Tokens   int
	Interval time.Duration
	Burst    int
}

// PerMinute is a Rate of n tokens per minute with a burst of n.
func PerMinute(n int) Rate {
	return Rate{Tokens: n, Interval: time.Minute, Burst: n}
}

// PerSecond is a Rate of n tokens per second with a burst of n.
func PerSecond(n int) Rate {
	return Rate{Tokens: n, Interval: time.Second, Burst: n}
}

// WithBurst returns a copy of r with bucket capacity set to burst. Use it to
// decouple burst tolerance from sustained rate, e.g. PerMinute(280).WithBurst(60).
func (r Rate) WithBurst(burst int) Rate {
	r.Burst = burst
	return r
}

// Valid reports whether r describes a usable bucket.
func (r Rate) Valid() bool {
	return r.Tokens > 0 && r.Interval > 0 && r.Burst > 0
}

// Result is the outcome of one Allow check.
type Result struct {
	// Allowed reports whether the caller may proceed.
	Allowed bool
	// Remaining is the tokens left in the bucket after this check.
	Remaining int
	// RetryAfter is how long until enough tokens free up to satisfy the request;
	// zero when Allowed.
	RetryAfter time.Duration
}

// Store holds bucket state and performs the atomic take. The method is
// unexported, so the implementation set is closed to this package: callers pick a
// constructor (NewRedisStore today) but cannot supply a subtly-wrong one. The
// seam is kept so an alternative backend can be added without touching callers.
type Store interface {
	take(ctx context.Context, key string, rate Rate, n int) (Result, error)
}

// Limiter enforces a single Rate across many keys via a Store. It is safe for
// concurrent use and cheap to construct; create one per logical guardrail at
// wiring time.
type Limiter struct {
	store   Store
	name    string
	rate    Rate
	metrics *metrics
}

// Option configures a Limiter.
type Option func(*Limiter)

// New binds name and rate to store. name namespaces the backing keys so
// unrelated limiters never share a bucket. Construction is infallible; an
// invalid rate surfaces as an error from Allow, not here.
func New(store Store, name string, rate Rate, opts ...Option) *Limiter {
	l := &Limiter{store: store, name: name, rate: rate, metrics: nil}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Allow consumes one token for key. A non-nil error means the Store was
// unreachable: the caller decides whether that fails open or closed — a Store
// outage is not a throttle, so do not treat !Allowed and a non-nil error alike.
func (l *Limiter) Allow(ctx context.Context, key string) (Result, error) {
	return l.AllowN(ctx, key, 1)
}

// AllowN consumes n tokens for key — charge a batch its real request count up
// front rather than looping Allow.
func (l *Limiter) AllowN(ctx context.Context, key string, n int) (Result, error) {
	if !l.rate.Valid() {
		return Result{Allowed: false, Remaining: 0, RetryAfter: 0}, fmt.Errorf("ratelimit %q: invalid rate %+v", l.name, l.rate)
	}
	if n <= 0 {
		return Result{Allowed: false, Remaining: 0, RetryAfter: 0}, fmt.Errorf("ratelimit %q: n must be positive, got %d", l.name, n)
	}
	if n > l.rate.Burst {
		// More than the bucket can ever hold: no wait makes it satisfiable, so
		// report it as such instead of a misleading finite RetryAfter.
		return Result{Allowed: false, Remaining: 0, RetryAfter: 0}, nil
	}

	res, err := l.store.take(ctx, keyPrefix+l.name+":"+key, l.rate, n)
	if err != nil {
		return res, fmt.Errorf("ratelimit %q: %w", l.name, err)
	}

	// Annotate the caller's span, which carries the org/project attribution, so
	// rate-limit decisions are queryable per org without this generic package
	// having to know the key's meaning.
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(
			attribute.String("ratelimit.name", l.name),
			attribute.Bool("ratelimit.allowed", res.Allowed),
			attribute.Int("ratelimit.remaining", res.Remaining),
		)
	}

	if l.metrics != nil {
		l.metrics.recordDecision(ctx, l.name, res.Allowed)
	}

	return res, nil
}
