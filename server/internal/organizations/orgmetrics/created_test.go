package orgmetrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	return rm
}

// Pins the contract a monitor queries: the instrument name, unit, scope, the
// attribute key, and that the counter carries no other attribute.
func TestCreatedRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := orgmetrics.NewCreated(testenv.NewLogger(t), provider)
	m.Record(t.Context(), orgmetrics.CreationSourceSignup)
	m.Record(t.Context(), orgmetrics.CreationSourceSignup)
	m.Record(t.Context(), orgmetrics.CreationSourceWorkOSSync)

	rm := collect(t, reader)
	require.Len(t, rm.ScopeMetrics, 1)
	require.Equal(t, "github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics", rm.ScopeMetrics[0].Scope.Name)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, "gram.organizations.created", got.Name)
	require.Equal(t, "{organization}", got.Unit)
	require.Equal(t, "gram.organization.creation_source", string(attr.OrganizationCreationSourceKey))

	metricdatatest.AssertEqual(t, metricdata.Metrics{
		Name:        "gram.organizations.created",
		Description: "Organizations created, by the flow that inserted the organization.",
		Unit:        "{organization}",
		Data: metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
			DataPoints: []metricdata.DataPoint[int64]{
				{Attributes: attribute.NewSet(attr.OrganizationCreationSource("signup")), Value: 2},
				{Attributes: attribute.NewSet(attr.OrganizationCreationSource("workos_sync")), Value: 1},
			},
		},
	}, got, metricdatatest.IgnoreTimestamp())
}

// Every value in the closed set is emitted verbatim; monitors are written
// against these strings.
func TestCreatedRecord_EmitsEveryKnownSource(t *testing.T) {
	t.Parallel()

	sources := map[orgmetrics.CreationSource]string{
		orgmetrics.CreationSourceSignup:        "signup",
		orgmetrics.CreationSourceAssistants:    "assistants",
		orgmetrics.CreationSourcePlatformMCP:   "platform_mcp",
		orgmetrics.CreationSourcePlatformAdmin: "platform_admin",
		orgmetrics.CreationSourceWorkOSSync:    "workos_sync",
		orgmetrics.CreationSourceLoginMirror:   "login_mirror",
	}

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m := orgmetrics.NewCreated(testenv.NewLogger(t), provider)

	want := make([]metricdata.DataPoint[int64], 0, len(sources))
	for source, value := range sources {
		require.Equal(t, value, string(source))
		m.Record(t.Context(), source)
		want = append(want, metricdata.DataPoint[int64]{Attributes: attribute.NewSet(attr.OrganizationCreationSource(value)), Value: 1})
	}

	rm := collect(t, reader)
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)
	sum, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	metricdatatest.AssertEqual(t, metricdata.Sum[int64]{
		Temporality: metricdata.CumulativeTemporality,
		IsMonotonic: true,
		DataPoints:  want,
	}, sum, metricdatatest.IgnoreTimestamp())
}

// An empty or unrecognized source is dropped rather than emitted as a label
// no monitor knows about.
func TestCreatedRecord_DropsUnknownSource(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m := orgmetrics.NewCreated(testenv.NewLogger(t), provider)

	m.Record(t.Context(), "")
	m.Record(t.Context(), "something_else")
	m.Record(t.Context(), orgmetrics.ProvisioningSource(""))

	rm := collect(t, reader)
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Empty(t, sum.DataPoints)
		}
	}
}

func TestProvisioningSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   orgmetrics.CreationSource
	}{
		{name: "signup", source: orgprovision.SourceSignup, want: orgmetrics.CreationSourceSignup},
		{name: "assistants", source: orgprovision.SourceAssistants, want: orgmetrics.CreationSourceAssistants},
		{name: "platform_mcp", source: orgprovision.SourcePlatformMCP, want: orgmetrics.CreationSourcePlatformMCP},
		{name: "platform_admin", source: orgprovision.SourcePlatformAdmin, want: orgmetrics.CreationSourcePlatformAdmin},
		{name: "empty", source: "", want: ""},
		{name: "unrecognized", source: "unknown", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, orgmetrics.ProvisioningSource(tc.source))
		})
	}
}
