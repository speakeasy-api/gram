package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMetrics_RecordHookEventDuration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := newMetrics(meterProvider, testenv.NewLogger(t))

	metrics.RecordHookEventDuration(ctx, "claude", "PreToolUse", hookMetricOutcomeAccepted, "acme", 150*time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	histogramPoint := findHookEventDurationPoint(t, rm)
	require.Equal(t, uint64(1), histogramPoint.Count)
	require.InDelta(t, 0.15, histogramPoint.Sum, 0.0001)

	value, ok := histogramPoint.Attributes.Value(attr.HookSourceKey)
	require.True(t, ok)
	require.Equal(t, "claude", value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.HookEventKey)
	require.True(t, ok)
	require.Equal(t, "PreToolUse", value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.OutcomeKey)
	require.True(t, ok)
	require.Equal(t, hookMetricOutcomeAccepted, value.AsString())

	value, ok = histogramPoint.Attributes.Value(attr.OrganizationSlugKey)
	require.True(t, ok)
	require.Equal(t, "acme", value.AsString())
}

func findHookEventDurationPoint(t *testing.T, rm metricdata.ResourceMetrics) metricdata.HistogramDataPoint[float64] {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHooksEventDuration {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, histogram.DataPoints, 1)
			return histogram.DataPoints[0]
		}
	}

	require.Failf(t, "metric not found", "missing metric %q", meterHooksEventDuration)
	return metricdata.HistogramDataPoint[float64]{}
}
