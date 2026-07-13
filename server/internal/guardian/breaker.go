package guardian

import (
	"context"
	"errors"
	"time"
)

// ErrCircuitOpen is the sentinel reason for requests denied because the
// circuit breaker guarding their partition is open. It surfaces from HTTP
// clients built with [WithResilience] wrapped in a [ResilienceError] (and, at
// the call site, a [*net/url.Error]), so match it with [errors.Is].
var ErrCircuitOpen = errors.New("circuit breaker open")

// BreakerState enumerates circuit breaker states. The string form is the
// metric-facing contract — it is recorded as an attribute on circuit breaker
// metrics — so renaming a state is a breaking change; the numeric values are
// internal.
type BreakerState int64

const (
	BreakerStateUnknown BreakerState = iota - 1
	BreakerStateOpen
	BreakerStateHalfOpen
	BreakerStateClosed
)

func (s BreakerState) String() string {
	switch s {
	case BreakerStateOpen:
		return "open"
	case BreakerStateHalfOpen:
		return "half-open"
	case BreakerStateClosed:
		return "closed"
	case BreakerStateUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// BreakerPolicy configures circuit breaking for a partition. Callers pass it
// on every Allow call; breakers lazily create per-partition state and rebuild
// it — resetting the failure window and state — when the policy seen for a
// partition changes. Derive a partition's policy from a single place so that
// concurrent callers cannot flap between conflicting policies.
type BreakerPolicy struct {
	// FailureRateThreshold is the fraction of failures in (0, 1] within
	// Window that opens the circuit.
	FailureRateThreshold float64

	// MinThroughput is the minimum number of recorded executions within
	// Window before FailureRateThreshold is evaluated, so cold or
	// low-traffic partitions do not trip on tiny samples.
	MinThroughput uint

	// Window is the rolling period over which the failure rate is measured.
	Window time.Duration

	// Delay is how long the circuit stays open before transitioning to
	// half-open and admitting trial executions.
	Delay time.Duration

	// SuccessThreshold is the number of consecutive successful half-open
	// executions required to close the circuit. A half-open failure reopens
	// it.
	SuccessThreshold uint

	// IncludeSubset opts the breaker's partition key into the subset segments
	// added via WithSubset. Leave it false (the default) so breaker state stays
	// scoped to upstream health (host); per-subset breakers dilute the failure
	// window and multiply half-open probes against a dead upstream. This can be
	// suitable when upstream failure is scoped to a specific tenant.
	IncludeSubset bool
}

// BreakerResult is the outcome of a circuit breaker admission check.
type BreakerResult struct {
	// State is the breaker state observed at admission time.
	State BreakerState

	// Allowed reports whether the execution was admitted.
	Allowed bool

	// RetryAfter is the time until an open circuit transitions to half-open
	// when the request was denied. It is -1 when the request was admitted
	// and 0 when denied by a half-open circuit whose trial capacity is
	// exhausted.
	RetryAfter time.Duration

	// Report records the execution outcome and must be called exactly once
	// when Allowed is true. Calling it again, or when Allowed is false, is
	// a no-op. It is never nil.
	Report func(failure bool)
}

// Breaker is a partitioned circuit breaker. Callers ask for admission with
// Allow and, when admitted, report the execution outcome via the returned
// [BreakerResult.Report].
type Breaker interface {
	Allow(ctx context.Context, key Partition, policy BreakerPolicy) (BreakerResult, error)
}
