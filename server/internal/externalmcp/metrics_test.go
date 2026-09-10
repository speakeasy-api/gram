package externalmcp_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHeaderRecoveryMetrics_CountersHaveNoDimensions(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics := externalmcp.NewMetrics(provider, testenv.NewLogger(t))

	// Request-derived baggage must not become metric dimensions.
	member, err := baggage.NewMember("gram.organization.id", "example-org")
	require.NoError(t, err)
	bag, err := baggage.New(member)
	require.NoError(t, err)
	ctx := baggage.ContextWithBaggage(t.Context(), bag)
	records := []struct {
		name   string
		record func(context.Context)
	}{
		{"externalmcp.header.mismatches", metrics.RecordHeaderMismatch},
		{"externalmcp.header.recovery.attempts", metrics.RecordRecoveryAttempt},
		{"externalmcp.header.recovery.successes", metrics.RecordRecoverySuccess},
		{"externalmcp.header.recovery.failures", metrics.RecordRecoveryFailure},
		{"externalmcp.header.recovery.exhaustions", metrics.RecordRecoveryExhaustion},
	}
	expected := make(map[string]int64, len(records))
	for i, rec := range records {
		for range i + 1 {
			rec.record(ctx)
		}
		expected[rec.name] = int64(i + 1)
	}

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	require.Len(t, data.ScopeMetrics, 1)
	scope := data.ScopeMetrics[0]
	require.Equal(t, "github.com/speakeasy-api/gram/server/internal/externalmcp", scope.Scope.Name)
	require.Len(t, scope.Metrics, len(records))
	for _, instrument := range scope.Metrics {
		count, exists := expected[instrument.Name]
		require.True(t, exists, "unexpected counter %q", instrument.Name)
		require.Equal(t, "{event}", instrument.Unit)
		require.NotEmpty(t, instrument.Description)
		sum, ok := instrument.Data.(metricdata.Sum[int64])
		require.True(t, ok, "%s must be an int64 counter", instrument.Name)
		require.True(t, sum.IsMonotonic)
		require.Equal(t, metricdata.CumulativeTemporality, sum.Temporality)
		require.Len(t, sum.DataPoints, 1)
		require.Equal(t, count, sum.DataPoints[0].Value, instrument.Name)
		require.Zero(t, sum.DataPoints[0].Attributes.Len(), instrument.Name)
		delete(expected, instrument.Name)
	}
	require.Empty(t, expected)
}

func TestHeaderRecoveryMetrics_NilAndZeroAreSafe(t *testing.T) {
	t.Parallel()
	for _, metrics := range []*externalmcp.Metrics{nil, {}} {
		require.NotPanics(t, func() {
			metrics.RecordHeaderMismatch(t.Context())
			metrics.RecordRecoveryAttempt(t.Context())
			metrics.RecordRecoverySuccess(t.Context())
			metrics.RecordRecoveryFailure(t.Context())
			metrics.RecordRecoveryExhaustion(t.Context())
		})
	}
}
