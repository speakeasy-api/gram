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
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/fixturerepo"
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

func TestLoadManyOpensAPageAndRecordsReadMetrics(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	first, second := uuid.New(), uuid.New()
	f.add(t, first)
	f.add(t, second)
	f.capture(t, first, second)

	reader := sdkmetric.NewManualReader()
	store, err := New(f.db, f.keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)

	opened, err := store.LoadMany(t.Context(), f.org, f.project, []uuid.UUID{first, second, first})
	require.NoError(t, err)
	require.Len(t, opened, 2)
	require.Equal(t, "sensitive example", opened[first].Content)
	require.Equal(t, "sensitive example", opened[second].Content)

	empty, err := store.LoadMany(t.Context(), f.org, f.project, nil)
	require.NoError(t, err)
	require.Empty(t, empty)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	require.Equal(t, map[string]int64{"success": 3}, countsByOutcome(t, collected, meterReadMessages))
}

func TestLoadManyLeavesOutMessagesWithoutAShadow(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	captured, uncaptured := uuid.New(), uuid.New()
	f.add(t, captured)
	f.add(t, uncaptured)
	f.capture(t, captured)

	reader := sdkmetric.NewManualReader()
	store, err := New(f.db, f.keys.Service, testenv.NewTracerProvider(t), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	require.NoError(t, err)

	opened, err := store.LoadMany(t.Context(), f.org, f.project, []uuid.UUID{captured, uncaptured})
	require.NoError(t, err)
	require.Len(t, opened, 1)
	require.Contains(t, opened, captured)

	none, err := store.LoadMany(t.Context(), f.org, f.project, []uuid.UUID{uncaptured})
	require.NoError(t, err)
	require.Empty(t, none)

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	require.Equal(t, map[string]int64{"success": 1, "plaintext_fallback": 2}, countsByOutcome(t, collected, meterReadMessages))
}

func TestLoadManyFailsThePageWhenAShadowIsStale(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)
	_, err := fixturerepo.New(f.db).ChangeContent(t.Context(), id, project(f.project))
	require.NoError(t, err)

	opened, err := f.store.LoadMany(t.Context(), f.org, f.project, []uuid.UUID{id})
	require.ErrorIs(t, err, ErrStale)
	require.Nil(t, opened)

	_, err = fixturerepo.New(f.db).ForgeSourceUpdatedAt(t.Context(), id, project(f.project), f.org)
	require.NoError(t, err)
	opened, err = f.store.LoadMany(t.Context(), f.org, f.project, []uuid.UUID{id})
	require.ErrorIs(t, err, ErrStale, "a forged matching source_updated_at cannot make an older body current")
	require.Nil(t, opened)
}

func TestLoadManyIsScopedToTheProjectAndOrganization(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	id := uuid.New()
	f.add(t, id)
	f.capture(t, id)

	// Another tenant's scope sees no shadow, so it opens nothing.
	opened, err := f.store.LoadMany(t.Context(), "org_other", f.project, []uuid.UUID{id})
	require.NoError(t, err)
	require.Empty(t, opened)
	opened, err = f.store.LoadMany(t.Context(), f.org, uuid.New(), []uuid.UUID{id})
	require.NoError(t, err)
	require.Empty(t, opened)
	_, err = f.store.LoadMany(t.Context(), f.org, f.project, []uuid.UUID{uuid.Nil})
	require.ErrorIs(t, err, ErrInvalid)
}

func countsByOutcome(t *testing.T, collected metricdata.ResourceMetrics, name string) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				result, ok := point.Attributes.Value(attr.OutcomeKey)
				require.True(t, ok)
				counts[result.AsString()] += point.Value
			}
		}
	}
	return counts
}

func TestClassifyCoversFailureOutcomes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want outcome
	}{
		{nil, outcomeSuccess},
		{ErrNotFound, outcomeNotFound},
		{fmt.Errorf("load: %w", ErrStale), outcomeStale},
		{fmt.Errorf("capture: %w", ErrChanged), outcomeChanged},
		{ErrInvalid, outcomeInvalid},
		{safe(context.DeadlineExceeded), outcomeError},
	} {
		require.Equal(t, tc.want, classify(tc.err))
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
