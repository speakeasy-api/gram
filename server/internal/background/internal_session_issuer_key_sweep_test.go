package background

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubIssuerKeySweeper struct {
	swept   int
	pending int
	err     error
}

func (s stubIssuerKeySweeper) SweepAllEMAIssuerKeys(context.Context) (int, int, error) {
	return s.swept, s.pending, s.err
}

func TestInternalSessionIssuerKeySweep(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	meterProvider := testenv.NewMeterProvider(t)

	swept, err := NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, nil).Sweep(t.Context())
	require.NoError(t, err, "a worker without KMS provisioning sweeps nothing")
	require.Zero(t, swept)

	swept, err = NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, stubIssuerKeySweeper{swept: 3, pending: 0, err: nil}).Sweep(t.Context())
	require.NoError(t, err)
	require.Equal(t, 3, swept)

	failure := errors.New("kms unavailable")
	_, err = NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, stubIssuerKeySweeper{swept: 1, pending: 1, err: failure}).Sweep(t.Context())
	require.ErrorIs(t, err, failure, "a failed sweep surfaces so Temporal retries it")
}

func TestInternalSessionIssuerKeySweepReportsPendingCleanup(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	failure := errors.New("kms unavailable")

	_, err := NewInternalSessionIssuerKeySweepActivities(testenv.NewLogger(t), meterProvider, stubIssuerKeySweeper{swept: 2, pending: 2, err: failure}).Sweep(t.Context())
	require.ErrorIs(t, err, failure)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	var pending []int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.internal_session_issuer.kms_cleanup.pending" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok)
			for _, point := range gauge.DataPoints {
				pending = append(pending, point.Value)
			}
		}
	}
	require.Equal(t, []int64{2}, pending, "keys still awaiting kms cleanup are reported")
}
