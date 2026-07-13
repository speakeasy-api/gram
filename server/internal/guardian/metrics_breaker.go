package guardian

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/metric"
)

const (
	// transitionCauseTraffic marks transitions driven by real request
	// outcomes flowing through the breaker (thresholds, delays, trial
	// outcomes).
	transitionCauseTraffic = "traffic"
	// transitionCauseReconfigured marks synthetic transitions recorded when a
	// policy change replaces a partition's breaker.
	transitionCauseReconfigured = "reconfigured"
)

type circuitBreakerMetrics struct {
	logger       *slog.Logger
	transitions  metric.Int64Counter
	requests     metric.Int64Counter
	openCircuits metric.Int64UpDownCounter
}

func newCircuitBreakerMetrics(logger *slog.Logger, meter metric.Meter) *circuitBreakerMetrics {
	logger = logger.With(attr.SlogComponent("otel"))

	transitions, err := meter.Int64Counter(
		"gram.circuit_breaker.transitions",
		metric.WithDescription("Number of circuit breaker state transitions"),
		metric.WithUnit("{transition}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create circuit breaker transitions counter", attr.SlogError(err))
	}

	requests, err := meter.Int64Counter(
		"gram.circuit_breaker.requests",
		metric.WithDescription("Number of requests admitted or rejected by a circuit breaker"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create circuit breaker requests counter", attr.SlogError(err))
	}

	openCircuits, err := meter.Int64UpDownCounter(
		"gram.circuit_breaker.open_circuits",
		metric.WithDescription("Number of circuit breakers currently open"),
		metric.WithUnit("{circuit_breaker}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create circuit breaker open circuits counter", attr.SlogError(err))
	}

	return &circuitBreakerMetrics{
		logger:       logger,
		transitions:  transitions,
		requests:     requests,
		openCircuits: openCircuits,
	}
}

func (m *circuitBreakerMetrics) recordTransition(ctx context.Context, key Partition, from, to BreakerState, cause string) {
	attrs := partitionAttrs(key)

	if m.transitions != nil {
		m.transitions.Add(ctx, 1, metric.WithAttributes(append(attrs,
			attr.ResilienceBreakerState(to.String()),
			attr.ResilienceBreakerPreviousState(from.String()),
			attr.ResilienceBreakerTransitionCause(cause),
		)...))
	}

	// The open-circuits gauge deliberately excludes the cause attribute: an
	// increment and its matching decrement must land on the same series to
	// cancel out, regardless of what caused each transition.

	if m.openCircuits != nil && from != to {
		if to == BreakerStateOpen {
			m.openCircuits.Add(ctx, 1, metric.WithAttributes(attrs...))
		} else if from == BreakerStateOpen {
			m.openCircuits.Add(ctx, -1, metric.WithAttributes(attrs...))
		}
	}
}

func (m *circuitBreakerMetrics) recordRequest(ctx context.Context, key Partition, allowed bool) {
	if m.requests == nil {
		return
	}

	outcome := outcomeRejected
	if allowed {
		outcome = outcomeAllowed
	}

	m.requests.Add(ctx, 1, metric.WithAttributes(append(partitionAttrs(key), attr.Outcome(outcome))...))
}
