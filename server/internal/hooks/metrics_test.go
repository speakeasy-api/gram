package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMetrics_RecordHookEventReceived(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics := newMetrics(meterProvider, testenv.NewLogger(t))

	metrics.RecordHookEventReceived(ctx, "claude", "PreToolUse", hookMetricOutcomeAccepted, "acme")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	point := findHookEventsReceivedPoint(t, rm)
	require.Equal(t, int64(1), point.Value)

	value, ok := point.Attributes.Value(attr.HookSourceKey)
	require.True(t, ok)
	require.Equal(t, "claude", value.AsString())

	value, ok = point.Attributes.Value(attr.HookEventKey)
	require.True(t, ok)
	require.Equal(t, "PreToolUse", value.AsString())

	value, ok = point.Attributes.Value(attr.OutcomeKey)
	require.True(t, ok)
	require.Equal(t, hookMetricOutcomeAccepted, value.AsString())

	value, ok = point.Attributes.Value(attr.OrganizationSlugKey)
	require.True(t, ok)
	require.Equal(t, "acme", value.AsString())
}

func findHookEventsReceivedPoint(t *testing.T, rm metricdata.ResourceMetrics) metricdata.DataPoint[int64] {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHooksEventsReceived {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			return sum.DataPoints[0]
		}
	}

	require.Failf(t, "metric not found", "missing metric %q", meterHooksEventsReceived)
	return metricdata.DataPoint[int64]{}
}
