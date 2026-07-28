package guardian

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"

	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// InProcBreaker is a [Breaker] whose state lives in process memory. Each
// replica observes its own failures, which converges on the same decision as
// shared state within a window without putting a network hop on the
// admission path.
type InProcBreaker struct {
	logger   *slog.Logger
	metrics  *circuitBreakerMetrics
	breakers *sync.Map
}

var _ Breaker = (*InProcBreaker)(nil)

func NewInProcBreaker(logger *slog.Logger, meterProvider metric.MeterProvider) *InProcBreaker {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/guardian")

	b := &InProcBreaker{
		logger:   logger,
		metrics:  newCircuitBreakerMetrics(logger, meter),
		breakers: new(sync.Map),
	}

	registerInstanceGauge(logger, meter,
		"gram.circuit_breaker.instances",
		"Number of circuit breaker instances resident in this process",
		"{circuit_breaker}",
		func() map[string]int64 {
			counts := make(map[string]int64)
			b.breakers.Range(func(_, val any) bool {
				if entry, ok := val.(*breakerEntry); ok {
					counts[entry.namespace]++
				}

				return true
			})

			return counts
		},
	)

	return b
}

type breakerEntry struct {
	cb          circuitbreaker.CircuitBreaker[any]
	fingerprint string
	namespace   string
}

func (b *InProcBreaker) Allow(ctx context.Context, key Partition, policy BreakerPolicy) (BreakerResult, error) {
	var zero BreakerResult

	switch {
	// NaN compares false against both bounds, so it must be rejected
	// explicitly or it would silently configure a breaker that never trips.
	case math.IsNaN(policy.FailureRateThreshold) || policy.FailureRateThreshold <= 0 || policy.FailureRateThreshold > 1:
		return zero, fmt.Errorf("in-proc breaker: failure rate threshold must be in (0, 1], got %v", policy.FailureRateThreshold)
	case policy.MinThroughput == 0:
		return zero, fmt.Errorf("in-proc breaker: min throughput must be positive")
	case policy.Window <= 0:
		return zero, fmt.Errorf("in-proc breaker: window must be positive, got %s", policy.Window)
	case policy.Delay <= 0:
		return zero, fmt.Errorf("in-proc breaker: delay must be positive, got %s", policy.Delay)
	case policy.SuccessThreshold == 0:
		return zero, fmt.Errorf("in-proc breaker: success threshold must be positive")
	}

	cb := b.breakerFor(ctx, key, policy)

	if !cb.TryAcquirePermit() {
		b.metrics.recordRequest(ctx, key, false)

		return BreakerResult{
			State:      toBreakerState(cb.State()),
			Allowed:    false,
			RetryAfter: cb.RemainingDelay(),
			Report:     func(bool) {},
		}, nil
	}

	b.metrics.recordRequest(ctx, key, true)

	var reported atomic.Bool

	return BreakerResult{
		State:      toBreakerState(cb.State()),
		Allowed:    true,
		RetryAfter: -1,
		Report: func(failure bool) {
			if !reported.CompareAndSwap(false, true) {
				return
			}
			if failure {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}
		},
	}, nil
}

func (b *InProcBreaker) breakerFor(ctx context.Context, key Partition, policy BreakerPolicy) circuitbreaker.CircuitBreaker[any] {
	// IncludeSubset only affects key derivation upstream, so it is not part
	// of the fingerprint.
	fingerprint := fmt.Sprintf("%g|%d|%s|%s|%d",
		policy.FailureRateThreshold, policy.MinThroughput, policy.Window, policy.Delay, policy.SuccessThreshold)

	if val, ok := b.breakers.Load(key.String()); ok {
		if entry, ok := val.(*breakerEntry); ok && entry.fingerprint == fingerprint {
			return entry.cb
		}
	}

	entry := &breakerEntry{cb: b.newBreaker(key, policy), fingerprint: fingerprint, namespace: key.Namespace()}
	actual, loaded := b.breakers.LoadOrStore(key.String(), entry)
	if loaded {
		if existing, ok := actual.(*breakerEntry); ok {
			if existing.fingerprint == fingerprint {
				return existing.cb
			}

			// The discarded breaker never transitions again, so settle the
			// open-circuits gauge with a compensating transition if it is
			// being replaced while open.
			if old := toBreakerState(existing.cb.State()); old == BreakerStateOpen {
				b.metrics.recordTransition(ctx, key, old, BreakerStateClosed, transitionCauseReconfigured)
			}
		}

		// The stored policy differs: replace it, resetting the partition's
		// window and state. This is meant as a reconfiguration path — see
		// the BreakerPolicy doc on deriving policies from a single place.
		b.breakers.Store(key.String(), entry)
	}

	return entry.cb
}

func (b *InProcBreaker) newBreaker(key Partition, policy BreakerPolicy) circuitbreaker.CircuitBreaker[any] {
	logger := b.logger.With(
		attr.SlogResilienceNamespace(key.Namespace()),
		attr.SlogResiliencePartition(key.Partition()),
		attr.SlogResilienceSubset(key.Subset()),
	)

	return circuitbreaker.NewBuilder[any]().
		WithFailureRateThreshold(policy.FailureRateThreshold, policy.MinThroughput, policy.Window).
		WithDelay(policy.Delay).
		WithSuccessThreshold(policy.SuccessThreshold).
		OnStateChanged(func(event circuitbreaker.StateChangedEvent) {
			from := toBreakerState(event.OldState)
			to := toBreakerState(event.NewState)
			b.metrics.recordTransition(event.Context(), key, from, to, transitionCauseTraffic)
			logger.InfoContext(event.Context(), "circuit breaker state changed",
				attr.SlogResilienceBreakerState(to.String()),
				attr.SlogResilienceBreakerPreviousState(from.String()),
			)
		}).
		Build()
}

func toBreakerState(state circuitbreaker.State) BreakerState {
	switch state {
	case circuitbreaker.ClosedState:
		return BreakerStateClosed
	case circuitbreaker.OpenState:
		return BreakerStateOpen
	case circuitbreaker.HalfOpenState:
		return BreakerStateHalfOpen
	default:
		return BreakerStateUnknown
	}
}
