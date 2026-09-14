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

// Pins the instrument name, its three attribute keys, and that one Record is one count.
func TestValidationRecord_PinsInstrumentAndDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewValidation(testenv.NewLogger(t), provider)
	m.Record(t.Context(), "https://idp.example.com/tenant-a", ValidationTriggerVerify, "rejected_by_member")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, "gram.remote_session.validation", got.Name)
	metricdatatest.AssertHasAttributes(t, got,
		attr.OAuthIssuer("https://idp.example.com/tenant-a"),
		attr.OAuthValidationTrigger(ValidationTriggerVerify),
		attr.Outcome("rejected_by_member"),
	)

	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok, "validation instrument must be an int64 counter")
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, int64(1), sum.DataPoints[0].Value)
}

// Every outcome lands on its own series, so a breakdown by outcome accounts for every probe.
func TestValidationRecord_SeparatesOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	m := NewValidation(testenv.NewLogger(t), provider)
	m.Record(t.Context(), "https://idp.example.com", ValidationTriggerConnect, "valid")
	m.Record(t.Context(), "https://idp.example.com", ValidationTriggerConnect, "valid")
	m.Record(t.Context(), "https://idp.example.com", ValidationTriggerKeepalive, "unknown")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	sum, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)

	counts := map[string]int64{}
	for _, dp := range sum.DataPoints {
		outcome, found := dp.Attributes.Value(attr.OutcomeKey)
		require.True(t, found)
		counts[outcome.AsString()] = dp.Value
	}
	require.Equal(t, map[string]int64{"valid": 2, "unknown": 1}, counts)
}

// A nil receiver and a nil instrument both degrade to no-ops, per the package convention.
func TestValidationRecord_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Validation
	m.Record(t.Context(), "https://idp.example.com", ValidationTriggerConnect, "valid")

	empty := &Validation{probes: nil}
	empty.Record(t.Context(), "https://idp.example.com", ValidationTriggerVerify, "valid")
}
