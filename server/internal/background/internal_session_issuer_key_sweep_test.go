package background

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubIssuerKeySweeper struct {
	progress internalsessionissuers.SweepProgress
	err      error
}

func (s stubIssuerKeySweeper) SweepEMAIssuerKeysFrom(context.Context, string, time.Duration) (internalsessionissuers.SweepProgress, error) {
	return s.progress, s.err
}

func progress(swept, pending int, next string, done bool) internalsessionissuers.SweepProgress {
	return internalsessionissuers.SweepProgress{Swept: swept, Pending: pending, Next: next, Done: done}
}

func TestInternalSessionIssuerKeySweep(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	meterProvider := testenv.NewMeterProvider(t)

	got, err := NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, nil).Sweep(t.Context(), "")
	require.NoError(t, err, "a worker without KMS provisioning sweeps nothing")
	require.True(t, got.Done)

	got, err = NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, stubIssuerKeySweeper{progress: progress(3, 0, "", true), err: nil}).Sweep(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, 3, got.Swept)

	failure := errors.New("kms unavailable")
	_, err = NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, stubIssuerKeySweeper{progress: progress(0, 1, "", false), err: failure}).Sweep(t.Context(), "")
	require.ErrorIs(t, err, failure, "a page with no progress fails so Temporal retries it")

	got, err = NewInternalSessionIssuerKeySweepActivities(logger, meterProvider, stubIssuerKeySweeper{progress: progress(2, 1, "org_b", false), err: failure}).Sweep(t.Context(), "")
	require.NoError(t, err, "a page that advanced keeps paging despite one failed organization")
	require.Equal(t, "org_b", got.Next)
}

func TestInternalSessionIssuerKeySweepReportsPendingCleanup(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	failure := errors.New("kms unavailable")

	_, err := NewInternalSessionIssuerKeySweepActivities(testenv.NewLogger(t), meterProvider, stubIssuerKeySweeper{progress: progress(0, 2, "", false), err: failure}).Sweep(t.Context(), "")
	require.ErrorIs(t, err, failure)
	require.Equal(t, []int64{2}, pendingGauge(t, reader), "keys still awaiting kms cleanup are reported")
}

func TestInternalSessionIssuerKeySweepSkipsUnknownPendingCount(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	failure := errors.New("database unavailable")

	_, err := NewInternalSessionIssuerKeySweepActivities(testenv.NewLogger(t), meterProvider, stubIssuerKeySweeper{progress: progress(0, -1, "", false), err: failure}).Sweep(t.Context(), "")
	require.ErrorIs(t, err, failure)
	require.Empty(t, pendingGauge(t, reader), "an unknown count records nothing")
}

func pendingGauge(t *testing.T, reader *sdkmetric.ManualReader) []int64 {
	t.Helper()
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
	return pending
}

func TestInternalSessionIssuerKeySweepWorkflowPagesByCursorAndStops(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var mu sync.Mutex
	var cursors []string
	env.RegisterActivityWithOptions(func(_ context.Context, after string) (internalsessionissuers.SweepProgress, error) {
		mu.Lock()
		defer mu.Unlock()
		cursors = append(cursors, after)
		return progress(1, 0, after+"x", false), nil
	}, activity.RegisterOptions{Name: InternalSessionIssuerKeySweepActivityName})

	env.ExecuteWorkflow(InternalSessionIssuerKeySweepWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{"", "x", "xx"}, cursors, "each page resumes from the last cursor, up to the page cap")
}
