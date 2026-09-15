package remotesessionmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Pins the wiring a noop meter cannot see: the instrument name, the three
// attribute keys the dashboard and the AIS-618 monitors query by, and that
// one Record is exactly one count.
func TestRefreshRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewRefresh(testenv.NewLogger(t), provider)
	m.Record(t.Context(), "https://idp.example.com/tenant-a", RefreshTriggerScheduled, RefreshOutcomeInvalidGrant)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, meterUpstreamRefresh, got.Name)
	metricdatatest.AssertHasAttributes(t, got,
		attr.OAuthIssuer("https://idp.example.com/tenant-a"),
		attr.OAuthRefreshTrigger(RefreshTriggerScheduled),
		attr.Outcome(RefreshOutcomeInvalidGrant),
	)

	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok, "upstream refresh instrument must be an int64 counter")
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
}

// A nil receiver and a nil instrument both degrade to no-ops rather than
// panicking, per the package convention.
func TestRefreshRecord_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Refresh
	m.Record(t.Context(), "https://idp.example.com", RefreshTriggerRequest, RefreshOutcomeRefreshed)

	empty := &Refresh{attempts: nil}
	empty.Record(t.Context(), "https://idp.example.com", RefreshTriggerRequest, RefreshOutcomeRefreshed)
}
