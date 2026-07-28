package guardian_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNoopBreaker_InstanceGauge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	b := guardian.NewNoopBreaker(testenv.NewLogger(t), meterProvider)
	policy := guardian.BreakerPolicy{
		FailureRateThreshold: 0.5,
		MinThroughput:        4,
		Window:               time.Minute,
		Delay:                time.Minute,
		SuccessThreshold:     1,
		IncludeSubset:        false,
	}

	require.Empty(t, instanceGaugeByNamespace(t, reader, "gram.circuit_breaker.instances"))

	// One count per distinct partition, attributed to its namespace, even
	// though the noop enforces nothing.
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

	// Repeat traffic on an existing partition does not grow the count.
	res, err := b.Allow(t.Context(), guardian.NewPartition("svc", "host-1"), policy)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	res.Report(false)

	require.Equal(t,
		map[string]int64{"svc": 2, "other": 1},
		instanceGaugeByNamespace(t, reader, "gram.circuit_breaker.instances"),
	)
}
