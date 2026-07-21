package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestLoggerRecordsCHWriteMetric verifies that synchronous ClickHouse writes
// through the telemetry Logger record the telemetry.clickhouse.write.duration
// histogram with operation and outcome attributes (DNO-521).
func TestLoggerRecordsCHWriteMetric(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, ti.chConn, enabled, enabled, nil)

	invURL, ok := shadowmcp.CanonicalizeInventoryURL("https://mcp.example.com/mcp")
	require.True(t, ok)
	require.NoError(t, telemLogger.UpsertShadowMCPInventoryURLs(ctx, []telemetry.ShadowMCPInventoryURL{{
		GramProjectID: uuid.NewString(),
		ServerURL:     invURL,
		ServerName:    "Example",
		SeenAt:        time.Date(2026, 6, 29, 15, 0, 0, 0, time.UTC),
	}}))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	var point *metricdata.HistogramDataPoint[float64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "telemetry.clickhouse.write.duration" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, histogram.DataPoints, 1)
			point = &histogram.DataPoints[0]
		}
	}
	require.NotNil(t, point, "expected telemetry.clickhouse.write.duration to be recorded")
	require.Equal(t, uint64(1), point.Count)

	value, ok := point.Attributes.Value(attr.TelemetryCHOperationKey)
	require.True(t, ok)
	require.Equal(t, "upsert_shadow_mcp_inventory_urls", value.AsString())

	value, ok = point.Attributes.Value(attr.OutcomeKey)
	require.True(t, ok)
	require.Equal(t, "success", value.AsString())
}
