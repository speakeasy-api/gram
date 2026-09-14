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

func TestIssuerMetadataRefreshRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewIssuerMetadataRefresh(testenv.NewLogger(t), provider)
	m.Record(t.Context(), "https://idp.example.com", IssuerMetadataRefreshOutcomeTransientFailure)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, meterIssuerMetadataRefresh, got.Name)
	metricdatatest.AssertHasAttributes(t, got,
		attr.OAuthIssuer("https://idp.example.com"),
		attr.Outcome(IssuerMetadataRefreshOutcomeTransientFailure),
	)

	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok, "issuer metadata refresh instrument must be an int64 counter")
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
}

func TestIssuerMetadataRefreshRecord_NilSafe(t *testing.T) {
	t.Parallel()

	var m *IssuerMetadataRefresh
	m.Record(t.Context(), "https://idp.example.com", IssuerMetadataRefreshOutcomeRefreshed)

	empty := &IssuerMetadataRefresh{attempts: nil}
	empty.Record(t.Context(), "https://idp.example.com", IssuerMetadataRefreshOutcomeRefreshed)
}
