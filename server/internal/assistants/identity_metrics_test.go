package assistants

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestIdentityAdmissionMetricModes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		execution *assistantidentity.Execution
		mode      string
		fallback  bool
	}{
		{name: "thread scoped", mode: identityAdmissionModeThreadScoped},
		{name: "unknown", execution: &assistantidentity.Execution{}, mode: identityAdmissionModeUnknown},
		{name: "workload", execution: &assistantidentity.Execution{Mode: assistantidentity.ExecutionWorkload}, mode: string(assistantidentity.ExecutionWorkload)},
		{name: "human fallback", execution: &assistantidentity.Execution{Mode: assistantidentity.ExecutionWorkloadHuman, FallbackReason: "not a metric label"}, mode: string(assistantidentity.ExecutionWorkloadHuman), fallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			counter, err := provider.Meter("test").Int64Counter("identity_admission")
			require.NoError(t, err)
			core := &ServiceCore{identityAdmission: counter}
			core.recordIdentityAdmission(t.Context(), tc.execution, nil)
			var result metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &result))
			require.Len(t, result.ScopeMetrics, 1)
			require.Len(t, result.ScopeMetrics[0].Metrics, 1)
			data := result.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
			require.Len(t, data.DataPoints, 1)
			point := data.DataPoints[0]
			require.EqualValues(t, 1, point.Value)
			require.Equal(t, attribute.NewSet(attribute.String("result", identityAdmissionIssued), attribute.String("mode", tc.mode), attribute.Bool("autonomous_fallback", tc.fallback)), point.Attributes)
		})
	}
}
