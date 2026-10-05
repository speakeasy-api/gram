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

// The four states partition every possible column value, and the S256 match is
// exact per RFC 7636 — a lowercase "s256" is a misconfiguration to surface,
// not a spelling to forgive.
func TestClassifyPKCESupport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		methods []string
		want    PKCESupportState
	}{
		{methods: nil, want: PKCESupportUncaptured},
		{methods: []string{}, want: PKCESupportNone},
		{methods: []string{"S256"}, want: PKCESupportSupported},
		{methods: []string{"plain", "S256"}, want: PKCESupportSupported},
		{methods: []string{"plain"}, want: PKCESupportUnsupported},
		{methods: []string{"s256"}, want: PKCESupportUnsupported},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ClassifyPKCESupport(tc.methods), "methods %v", tc.methods)
	}
}

// Pins the wiring a noop meter cannot see: the instrument name and the two
// attribute keys the AIS-566 enforcement decision will query by.
func TestAuthorizeRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewAuthorize(testenv.NewLogger(t), provider)
	m.Record(t.Context(), "https://idp.example.com/tenant-a", PKCESupportUncaptured)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, meterUpstreamAuthorize, got.Name)
	metricdatatest.AssertHasAttributes(t, got,
		attr.OAuthIssuer("https://idp.example.com/tenant-a"),
		attr.PKCESupport(PKCESupportUncaptured),
	)
}

// A nil receiver and a nil instrument both degrade to no-ops rather than
// panicking, per the package convention.
func TestAuthorizeRecord_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Authorize
	m.Record(t.Context(), "okta-prod", PKCESupportSupported)

	m.RecordUnanswered(t.Context(), "okta-prod")

	empty := &Authorize{flows: nil, unanswered: nil}
	empty.Record(t.Context(), "okta-prod", PKCESupportSupported)
	empty.RecordUnanswered(t.Context(), "okta-prod")
}

// The unanswered-leg counter carries the issuer and nothing else, so no user
// identifier can reach a metric label.
func TestAuthorizeRecordUnanswered_IssuerIsTheOnlyDimension(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewAuthorize(testenv.NewLogger(t), provider)
	m.RecordUnanswered(t.Context(), "https://idp.example.com")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, meterUpstreamAuthorizeUnanswered, got.Name)
	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
	require.Equal(t, 1, sum.DataPoints[0].Attributes.Len())
	metricdatatest.AssertHasAttributes(t, got, attr.OAuthIssuer("https://idp.example.com"))
}
