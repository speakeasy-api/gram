package transcriptstore

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCaptureRecordsOutcomeMetrics(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	stored := uuid.New()
	f.add(t, stored)

	reader := sdkmetric.NewManualReader()
	store, err := New(f.db, f.keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)

	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary matches how the chat writer calls Capture; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	require.NoError(t, store.Capture(t.Context(), tx, f.org, f.project, []uuid.UUID{stored}))
	require.NoError(t, tx.Commit(t.Context()))

	missing, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: caller-owned transaction boundary matches how the chat writer calls Capture; fixture data uses generated queries.
	require.NoError(t, err)
	defer func() { _ = missing.Rollback(t.Context()) }()
	require.ErrorIs(t, store.Capture(t.Context(), missing, f.org, f.project, []uuid.UUID{uuid.New(), uuid.New()}), ErrNotFound)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))

	messages := map[string]int64{}
	durations := map[string]uint64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				require.Equal(t, meterCaptureMessages, m.Name)
				for _, point := range data.DataPoints {
					outcome, ok := point.Attributes.Value(attr.OutcomeKey)
					require.True(t, ok)
					messages[outcome.AsString()] += point.Value
				}
			case metricdata.Histogram[float64]:
				require.Equal(t, meterCaptureDuration, m.Name)
				for _, point := range data.DataPoints {
					outcome, ok := point.Attributes.Value(attr.OutcomeKey)
					require.True(t, ok)
					durations[outcome.AsString()] += point.Count
				}
			}
		}
	}

	require.Equal(t, map[string]int64{"success": 1, "not_found": 2}, messages)
	require.Equal(t, map[string]uint64{"success": 1, "not_found": 1}, durations)
}

func TestClassifyCaptureCoversRollbackOutcomes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want captureOutcome
	}{
		{nil, captureOutcomeSuccess},
		{ErrNotFound, captureOutcomeNotFound},
		{fmt.Errorf("capture: %w", ErrChanged), captureOutcomeChanged},
		{safe(context.DeadlineExceeded), captureOutcomeError},
	} {
		require.Equal(t, tc.want, classifyCapture(tc.err))
	}
}

func TestCaptureRejectsInvalidArgumentsBeforeTelemetry(t *testing.T) {
	t.Parallel()
	// A zero-value store has no tracer or metrics; invalid input must not reach them.
	ids := []uuid.UUID{uuid.New()}
	require.ErrorIs(t, (&Store{}).Capture(t.Context(), nil, "org", uuid.New(), ids), ErrInvalid)
	require.ErrorIs(t, (&Store{}).Capture(t.Context(), nil, "", uuid.New(), ids), ErrInvalid)
	require.ErrorIs(t, (&Store{}).Capture(t.Context(), nil, "org", uuid.Nil, ids), ErrInvalid)
}
