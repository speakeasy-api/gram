package guardian_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNoopLimiter_BucketGauge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	l := guardian.NewNoopLimiter(testenv.NewLogger(t), meterProvider)
	limit := guardian.PerSecond(10)

	require.Empty(t, instanceGaugeByNamespace(t, reader, "gram.rate_limit.buckets"))

	// One count per distinct partition, attributed to its namespace, even
	// though the noop enforces nothing.
	for _, key := range []guardian.Partition{
		guardian.NewPartition("egress", "org-1"),
		guardian.NewPartition("egress", "org-2"),
		guardian.NewPartition("chat", "org-1"),
	} {
		res, err := l.AllowN(t.Context(), key, limit, 1)
		require.NoError(t, err)
		require.Equal(t, 1, res.Allowed)
	}

	require.Equal(t,
		map[string]int64{"egress": 2, "chat": 1},
		instanceGaugeByNamespace(t, reader, "gram.rate_limit.buckets"),
	)

	// Repeat traffic on an existing partition, including with a changed
	// limit, does not grow the count.
	_, err := l.AllowN(t.Context(), guardian.NewPartition("egress", "org-1"), guardian.PerSecond(100), 1)
	require.NoError(t, err)

	require.Equal(t,
		map[string]int64{"egress": 2, "chat": 1},
		instanceGaugeByNamespace(t, reader, "gram.rate_limit.buckets"),
	)
}
