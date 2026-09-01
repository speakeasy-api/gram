// External test package on purpose: the instrument name below is asserted as
// a literal rather than read from the unexported constant, because it is a
// wire contract. Every dashboard and monitor over CIMD admission is written
// against that string, so a rename has to fail here.
package admission_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const instrumentAdmissionDecisions = "cimd.admission.decisions"

func newObservedMetrics(t *testing.T) (*admission.Metrics, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return admission.NewMetrics(provider, testenv.NewLogger(t)), reader
}

func decisionPoints(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != instrumentAdmissionDecisions {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "the admission instrument must be an int64 counter")
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}
	return points
}

// TestMetrics_RecordsOneShadowOutcome covers the recording surface only: a
// shadow verdict reaches the counter as an ordinary admission, under both
// dimensions and under no others. That one request yields one point is a
// property of the callers, and is pinned end to end against a real authorize
// in the mcp package.
func TestMetrics_RecordsOneShadowOutcome(t *testing.T) {
	t.Parallel()

	metrics, reader := newObservedMetrics(t)
	metrics.RecordAdmitted(t.Context(), admission.ModeOpen, admission.AdmitOpenNotListed)

	points := decisionPoints(t, reader)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModeOpen),
		attr.CIMDAdmissionOutcome(string(admission.AdmitOpenNotListed)),
	)])
}

// TestMetrics_SeparatesModesAndOutcomes: both dimensions have to survive onto
// the point, or the counter cannot answer either question it exists for —
// which policy was in force, and what it decided.
func TestMetrics_SeparatesModesAndOutcomes(t *testing.T) {
	t.Parallel()

	metrics, reader := newObservedMetrics(t)
	metrics.RecordAdmitted(t.Context(), admission.ModeOpen, admission.AdmitCatalogExact)
	metrics.RecordAdmitted(t.Context(), admission.ModeOpen, admission.AdmitCatalogExact)
	metrics.RecordAdmitted(t.Context(), admission.ModePresets, admission.AdmitCatalogExact)
	metrics.RecordDenied(t.Context(), admission.ModePresets, admission.DenialNotListed)

	points := decisionPoints(t, reader)
	require.Len(t, points, 3)
	require.Equal(t, int64(2), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModeOpen),
		attr.CIMDAdmissionOutcome(string(admission.AdmitCatalogExact)),
	)])
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModePresets),
		attr.CIMDAdmissionOutcome(string(admission.AdmitCatalogExact)),
	)])
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.CIMDAdmissionMode(admission.ModePresets),
		attr.CIMDAdmissionOutcome(string(admission.DenialNotListed)),
	)])
}

// TestMetrics_NilIsInert: admission runs on an unauthenticated endpoint, so
// a metrics value that failed to build must degrade to silence rather than
// panic mid-flow.
func TestMetrics_NilIsInert(t *testing.T) {
	t.Parallel()

	var metrics *admission.Metrics
	require.NotPanics(t, func() {
		metrics.RecordAdmitted(t.Context(), admission.ModeOpen, admission.AdmitOpen)
		metrics.RecordDenied(t.Context(), admission.ModePresets, admission.DenialNotListed)
	})
}
