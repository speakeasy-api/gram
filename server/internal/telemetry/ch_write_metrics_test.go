package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestLoggerWritesRecordConnLevelMetric verifies the consolidated
// instrumentation story for synchronous ClickHouse writes (DNO-521 →
// DNO-602): the Logger no longer wraps writes in its own span/metric — every
// ClickHouse call is spanned and measured at the connection layer by
// o11y.TraceClickhouseConn, labeled with the physical table and issuing repo
// function. The upsert issues one point-SELECT per URL plus the insert, so
// each call yields its own duration sample.
func TestLoggerWritesRecordConnLevelMetric(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tracedConn := o11y.TraceClickhouseConn(ti.chConn, testenv.NewTracerProvider(t), meterProvider, testenv.NewLogger(t))
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	telemLogger := telemetry.NewLogger(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, tracedConn, enabled, enabled, nil)

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

	var points []metricdata.HistogramDataPoint[float64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			require.NotEqual(t, "telemetry.clickhouse.write.duration", m.Name,
				"the write-duration histogram was consolidated into clickhouse.client.query.duration and must not resurface")
			if m.Name != "clickhouse.client.query.duration" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			points = append(points, histogram.DataPoints...)
		}
	}
	require.NotEmpty(t, points, "the write must be measured at the connection layer")

	insertSamples := uint64(0)
	for _, point := range points {
		table, ok := point.Attributes.Value("gram.clickhouse.table")
		require.True(t, ok)
		require.Equal(t, "shadow_mcp_inventory_urls", table.AsString())

		outcome, ok := point.Attributes.Value("gram.outcome")
		require.True(t, ok)
		require.Equal(t, "success", outcome.AsString())

		// Operations name the innermost issuing repo function
		// (first-frame-wins): the upsert's batched lookup and insert helpers.
		operation, ok := point.Attributes.Value("gram.clickhouse.operation")
		require.True(t, ok)
		require.True(t, strings.HasPrefix(operation.AsString(), "Queries."), "got operation %q", operation.AsString())
		if strings.Contains(operation.AsString(), "insertShadowMCPInventoryURLRows") {
			insertSamples += point.Count
		}
	}
	require.Positive(t, insertSamples, "the upsert's insert must be attributed to its issuing repo function")
}
