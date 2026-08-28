package mcpmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestIdentityCoverageRecord_PinsInstrumentAndDimensions pins the wiring a
// noop meter cannot see: the instrument name and the three bounded attribute
// keys the coverage dashboards will query by.
func TestIdentityCoverageRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	c := NewIdentityCoverageCounter(meter, testenv.NewLogger(t))
	c.Record(t.Context(), KillswitchSurfacePrivateProxy, KillswitchIdentityActiveUser, KillswitchResourceCanonicalServer)

	got := collectMetric(t, reader, InstrumentMCPToolCallKillswitchIdentity)
	metricdatatest.AssertHasAttributes(t, got,
		attr.McpKillswitchSurface(KillswitchSurfacePrivateProxy),
		attr.McpKillswitchIdentityClass(KillswitchIdentityActiveUser),
		attr.McpKillswitchResourceClass(KillswitchResourceCanonicalServer),
	)

	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok, "coverage instrument must be an int64 counter")
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
}

// TestIdentityCoverageRecord_ClampsAtRecordSite pins that every dimension is
// clamped inside Record: out-of-set values must land in the bounded fallback
// buckets so no identifier, URL, or error text can ever mint a metric series.
func TestIdentityCoverageRecord_ClampsAtRecordSite(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	c := NewIdentityCoverageCounter(meter, testenv.NewLogger(t))
	c.Record(t.Context(),
		KillswitchCoverageSurface("https://mcp.example.com/mcp/demo"),
		KillswitchIdentityClass("user_01J8SOMEUSER"),
		KillswitchResourceClass("pg: connection refused"),
	)

	metricdatatest.AssertHasAttributes(t, collectMetric(t, reader, InstrumentMCPToolCallKillswitchIdentity),
		attr.McpKillswitchSurface(KillswitchSurfaceHosted),
		attr.McpKillswitchIdentityClass(KillswitchIdentityUnavailable),
		attr.McpKillswitchResourceClass(KillswitchResourceUnavailable),
	)
}

// TestIdentityCoverageRecord_NilSafe pins the documented contract that a nil
// counter and one whose instrument failed to construct both record safely.
func TestIdentityCoverageRecord_NilSafe(t *testing.T) {
	t.Parallel()

	var nilCounter *IdentityCoverageCounter
	nilCounter.Record(t.Context(), KillswitchSurfaceHosted, KillswitchIdentityActiveUser, KillswitchResourceCanonicalServer)

	empty := &IdentityCoverageCounter{calls: nil}
	empty.Record(t.Context(), KillswitchSurfaceHosted, KillswitchIdentityActiveUser, KillswitchResourceCanonicalServer)
}
