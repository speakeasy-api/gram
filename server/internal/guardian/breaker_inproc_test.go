package guardian_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTestBreaker(t *testing.T) *guardian.InProcBreaker {
	t.Helper()

	return guardian.NewInProcBreaker(testenv.NewLogger(t), testenv.NewMeterProvider(t))
}

func TestInProcBreaker_Allow_TripsOnFailureRate(t *testing.T) {
	t.Parallel()

	b := newTestBreaker(t)
	key := guardian.NewPartition("svc", "host-1")
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 0.5,
		MinThroughput:        4,
		Window:               time.Minute,
		Delay:                time.Hour,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	// Failures below MinThroughput must not trip the breaker.
	for i := range 3 {
		res, err := b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed, "request %d should be admitted", i)
		require.Equal(t, guardian.BreakerStateClosed, res.State)
		require.Equal(t, time.Duration(-1), res.RetryAfter)
		res.Report(true)
	}

	// The 4th failure reaches MinThroughput with a 100% failure rate and
	// opens the circuit.
	res, err := b.Allow(t.Context(), key, policy)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	res.Report(true)

	res, err = b.Allow(t.Context(), key, policy)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Equal(t, guardian.BreakerStateOpen, res.State)
	require.Positive(t, res.RetryAfter)
	require.Greater(t, res.RetryAfter, 50*time.Minute, "RetryAfter should reflect the open delay")

	// Other partitions are unaffected.
	other, err := b.Allow(t.Context(), guardian.NewPartition("svc", "host-2"), policy)
	require.NoError(t, err)
	require.True(t, other.Allowed)
	other.Report(false)
}

func TestInProcBreaker_Allow_SuccessesKeepCircuitClosed(t *testing.T) {
	t.Parallel()

	b := newTestBreaker(t)
	key := guardian.NewPartition("svc", "host-1")
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 0.7,
		MinThroughput:        4,
		Window:               time.Minute,
		Delay:                time.Hour,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	// Half the traffic failing peaks at a 3/5 failure rate, under the 70%
	// threshold (the rate comparison is inclusive).
	for i := range 12 {
		res, err := b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed, "request %d should be admitted", i)
		res.Report(i%2 == 0)
	}
}

func TestInProcBreaker_Allow_HalfOpenClosesAfterSuccesses(t *testing.T) {
	t.Parallel()

	// synctest's fake clock drives the breaker through the open delay
	// deterministically: failsafe reads the (virtualized) wall clock.
	synctest.Test(t, func(t *testing.T) {
		b := newTestBreaker(t)
		key := guardian.NewPartition("svc", "host-1")
		policy := guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                25 * time.Millisecond,
			SuccessThreshold:     2,
			IncludeSubset:        false,
		}

		tripBreaker(t, b, key, policy)

		// After the delay the circuit half-opens and admits trial
		// executions; SuccessThreshold successes close it.
		time.Sleep(policy.Delay) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep

		res, err := b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.Equal(t, guardian.BreakerStateHalfOpen, res.State)
		res.Report(false)

		res, err = b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.Equal(t, guardian.BreakerStateHalfOpen, res.State)
		res.Report(false)

		res, err = b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.Equal(t, guardian.BreakerStateClosed, res.State)
		res.Report(false)
	})
}

func TestInProcBreaker_Allow_HalfOpenFailureReopens(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		b := newTestBreaker(t)
		key := guardian.NewPartition("svc", "host-1")
		policy := guardian.BreakerPolicy{
			FailureRateThreshold: 1,
			MinThroughput:        2,
			Window:               time.Minute,
			Delay:                25 * time.Millisecond,
			SuccessThreshold:     1,
			IncludeSubset:        false,
		}

		tripBreaker(t, b, key, policy)

		// Fail the half-open trial execution: the circuit reopens for a
		// full delay.
		time.Sleep(policy.Delay) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep

		res, err := b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.Equal(t, guardian.BreakerStateHalfOpen, res.State)
		res.Report(true)

		res, err = b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.False(t, res.Allowed)
		require.Equal(t, guardian.BreakerStateOpen, res.State)
		require.Equal(t, policy.Delay, res.RetryAfter)
	})
}

func TestInProcBreaker_Allow_PolicyChangeResetsPartition(t *testing.T) {
	t.Parallel()

	b := newTestBreaker(t)
	key := guardian.NewPartition("svc", "host-1")
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 1,
		MinThroughput:        2,
		Window:               time.Minute,
		Delay:                time.Hour,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	tripBreaker(t, b, key, policy)

	res, err := b.Allow(t.Context(), key, policy)
	require.NoError(t, err)
	require.False(t, res.Allowed)

	// A changed policy rebuilds the partition's breaker from scratch.
	changed := policy
	changed.Window = 2 * time.Minute
	res, err = b.Allow(t.Context(), key, changed)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Equal(t, guardian.BreakerStateClosed, res.State)
	res.Report(false)
}

func TestInProcBreaker_Allow_PolicyChangeSettlesOpenCircuitsGauge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	b := guardian.NewInProcBreaker(testenv.NewLogger(t), meterProvider)
	key := guardian.NewPartition("svc", "host-1")
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 1,
		MinThroughput:        2,
		Window:               time.Minute,
		Delay:                time.Hour,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	tripBreaker(t, b, key, policy)
	require.Equal(t, int64(1), openCircuitsGauge(t, reader))

	// Replacing an open partition's breaker must settle the gauge: the
	// discarded breaker never records its own open -> closed transition.
	changed := policy
	changed.Window = 2 * time.Minute
	res, err := b.Allow(t.Context(), key, changed)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	res.Report(false)

	require.Equal(t, int64(0), openCircuitsGauge(t, reader))

	// The settling transition is synthetic and must be distinguishable from
	// transitions driven by the breaker's own state machine.
	require.Equal(t, int64(1), transitionCount(t, reader, "reconfigured"))
	require.Equal(t, int64(1), transitionCount(t, reader, "traffic"))
}

func TestInProcBreaker_InstanceGauge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	b := guardian.NewInProcBreaker(testenv.NewLogger(t), meterProvider)
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 0.5,
		MinThroughput:        4,
		Window:               time.Minute,
		Delay:                time.Minute,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	require.Empty(t, instanceGaugeByNamespace(t, reader, "gram.circuit_breaker.instances"))

	// One breaker per distinct partition, attributed to its namespace.
	for _, key := range []guardian.Partition{
		guardian.NewPartition("svc", "host-1"),
		guardian.NewPartition("svc", "host-2"),
		guardian.NewPartition("other", "host-1"),
	} {
		res, err := b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		require.True(t, res.Allowed)
		res.Report(false)
	}

	require.Equal(t,
		map[string]int64{"svc": 2, "other": 1},
		instanceGaugeByNamespace(t, reader, "gram.circuit_breaker.instances"),
	)

	// Repeat traffic and policy changes reuse or replace a partition's
	// breaker; neither grows the count.
	changed := policy
	changed.Window = 2 * time.Minute
	res, err := b.Allow(t.Context(), guardian.NewPartition("svc", "host-1"), changed)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	res.Report(false)

	require.Equal(t,
		map[string]int64{"svc": 2, "other": 1},
		instanceGaugeByNamespace(t, reader, "gram.circuit_breaker.instances"),
	)
}

func TestInProcBreaker_Allow_ReportIsIdempotent(t *testing.T) {
	t.Parallel()

	b := newTestBreaker(t)
	key := guardian.NewPartition("svc", "host-1")
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 1,
		MinThroughput:        2,
		Window:               time.Minute,
		Delay:                time.Hour,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	// Double-reporting a single admission must count once: one failure is
	// below MinThroughput, so the circuit stays closed.
	res, err := b.Allow(t.Context(), key, policy)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	res.Report(true)
	res.Report(true)

	res, err = b.Allow(t.Context(), key, policy)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Equal(t, guardian.BreakerStateClosed, res.State)
	res.Report(false)
}

func TestInProcBreaker_Allow_InvalidPolicy(t *testing.T) {
	t.Parallel()

	b := newTestBreaker(t)
	key := guardian.NewPartition("svc", "host-1")
	valid := guardian.BreakerPolicy{
		FailureRateThreshold: 0.5,
		MinThroughput:        1,
		Window:               time.Minute,
		Delay:                time.Minute,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	for _, mutate := range []func(*guardian.BreakerPolicy){
		func(p *guardian.BreakerPolicy) { p.FailureRateThreshold = 0 },
		func(p *guardian.BreakerPolicy) { p.FailureRateThreshold = 1.5 },
		func(p *guardian.BreakerPolicy) { p.FailureRateThreshold = math.NaN() },
		func(p *guardian.BreakerPolicy) { p.MinThroughput = 0 },
		func(p *guardian.BreakerPolicy) { p.Window = 0 },
		func(p *guardian.BreakerPolicy) { p.Delay = 0 },
		func(p *guardian.BreakerPolicy) { p.SuccessThreshold = 0 },
	} {
		policy := valid
		mutate(&policy)
		_, err := b.Allow(t.Context(), key, policy)
		require.Error(t, err)
	}
}

func TestInProcBreaker_Allow_Concurrent(t *testing.T) {
	t.Parallel()

	b := newTestBreaker(t)
	key := guardian.NewPartition("svc", "host-1")
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 0.9,
		MinThroughput:        1000,
		Window:               time.Minute,
		Delay:                time.Minute,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	var group errgroup.Group
	for range 8 {
		group.Go(func() error {
			for range 50 {
				res, err := b.Allow(t.Context(), key, policy)
				if err != nil {
					return fmt.Errorf("allow: %w", err)
				}
				if res.Allowed {
					res.Report(false)
				}
			}

			return nil
		})
	}

	require.NoError(t, group.Wait())
}

// openCircuitsGauge collects metrics and returns the current value of the
// gram.circuit_breaker.open_circuits up-down counter summed across all
// attribute sets.
func openCircuitsGauge(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gram.circuit_breaker.open_circuits" {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "open_circuits should export as an int64 sum")
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}

	return total
}

// transitionCount collects metrics and returns the total of the
// gram.circuit_breaker.transitions counter across datapoints tagged with the
// given transition cause.
func transitionCount(t *testing.T, reader *sdkmetric.ManualReader, cause string) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gram.circuit_breaker.transitions" {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "transitions should export as an int64 sum")
			for _, dp := range sum.DataPoints {
				v, ok := dp.Attributes.Value(attr.ResilienceBreakerTransitionCauseKey)
				if ok && v.AsString() == cause {
					total += dp.Value
				}
			}
		}
	}

	return total
}

// instanceGaugeByNamespace collects metrics and returns the named observable
// gauge's value per resilience namespace. It is shared with the in-proc rate
// limiter tests, which measure gram.rate_limit.buckets the same way.
func instanceGaugeByNamespace(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	counts := make(map[string]int64)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}

			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "%s should export as an int64 gauge", name)
			for _, dp := range gauge.DataPoints {
				v, ok := dp.Attributes.Value(attr.ResilienceNamespaceKey)
				require.True(t, ok, "instance gauge datapoints should carry a namespace attribute")
				counts[v.AsString()] = dp.Value
			}
		}
	}

	return counts
}

// tripBreaker drives the partition's failure rate to 100% until the circuit
// opens.
func tripBreaker(t *testing.T, b *guardian.InProcBreaker, key guardian.Partition, policy guardian.BreakerPolicy) {
	t.Helper()

	for range int(policy.MinThroughput) + 1 {
		res, err := b.Allow(t.Context(), key, policy)
		require.NoError(t, err)
		if !res.Allowed {
			return
		}
		res.Report(true)
	}

	res, err := b.Allow(t.Context(), key, policy)
	require.NoError(t, err)
	require.False(t, res.Allowed, "breaker should have tripped")
}
