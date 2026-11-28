// Package orgmetricstest reads the organizations created counter in tests of
// the flows that insert organizations.
package orgmetricstest

import (
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Reader collects what a Created built by New recorded.
type Reader struct {
	reader *sdkmetric.ManualReader
}

// New returns a Created backed by its own meter provider, and a Reader over
// it, so parallel tests never observe each other's counts.
func New(t *testing.T) (*orgmetrics.Created, *Reader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	return orgmetrics.NewCreated(testenv.NewLogger(t), provider), &Reader{reader: reader}
}

// Counts returns the recorded total for each creation source. A source with
// nothing recorded is absent.
func (r *Reader) Counts(t *testing.T) map[orgmetrics.CreationSource]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, r.reader.Collect(t.Context(), &rm))

	counts := make(map[orgmetrics.CreationSource]int64)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %q is not an int64 sum", m.Name)
			for _, point := range sum.DataPoints {
				source, ok := point.Attributes.Value(attr.OrganizationCreationSourceKey)
				require.True(t, ok, "data point has no creation source")
				counts[orgmetrics.CreationSource(source.AsString())] += point.Value
			}
		}
	}
	return counts
}

// RequireOnly asserts that want creations were recorded under source and none
// under any other source.
func (r *Reader) RequireOnly(t *testing.T, source orgmetrics.CreationSource, want int64) {
	t.Helper()

	// Built by assignment: the exhaustive linter rejects a map literal keyed by
	// CreationSource unless it lists every source.
	expected := make(map[orgmetrics.CreationSource]int64)
	expected[source] = want
	require.Equal(t, expected, r.Counts(t))
}
