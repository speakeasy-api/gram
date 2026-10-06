package remotesessionmetrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Pins the instrument names and the two dimensions dashboards query by.
func TestScopeResolution_PinsInstrumentsAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewScopeResolution(testenv.NewLogger(t), provider)
	m.Record(t.Context(), ScopeSourceLiveResource, protectedresource.ProbeOutcomeFetched)
	m.RecordProbe(t.Context(), protectedresource.ProbeOutcomeFetched, 250*time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	byName := map[string]metricdata.Metrics{}
	for _, metric := range rm.ScopeMetrics[0].Metrics {
		byName[metric.Name] = metric
	}

	// Dashboards query these names and keys literally.
	counter, ok := byName["gram.remote_session.scope_resolution"]
	require.True(t, ok)
	metricdatatest.AssertHasAttributes(t, counter,
		attribute.String("gram.oauth.scope_source", "live_resource"),
		attribute.String("gram.oauth.resource_probe_outcome", "fetched"),
	)

	histogram, ok := byName["gram.remote_session.resource_probe.duration"]
	require.True(t, ok)
	require.Equal(t, "s", histogram.Unit)
	metricdatatest.AssertHasAttributes(t, histogram, attribute.String("gram.oauth.resource_probe_outcome", "fetched"))
	data, ok := histogram.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, data.DataPoints, 1)
	require.InDelta(t, 0.25, data.DataPoints[0].Sum, 1e-9)
}

func TestScopeResolution_NilSafe(t *testing.T) {
	t.Parallel()

	var m *ScopeResolution
	m.Record(t.Context(), ScopeSourceNone, protectedresource.ProbeOutcomeNotApplicable)
	m.RecordProbe(t.Context(), protectedresource.ProbeOutcomeTimeout, time.Second)

	empty := &ScopeResolution{resolutions: nil, probeDuration: nil}
	empty.Record(t.Context(), ScopeSourceNone, protectedresource.ProbeOutcomeNotApplicable)
	empty.RecordProbe(t.Context(), protectedresource.ProbeOutcomeTimeout, time.Second)
}
