package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
)

func TestParseFlagsDefaultsToDryRun(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://localhost/gram")
	projectID := uuid.MustParse("019b1000-0000-7000-8000-000000000001")

	cfg, err := parseFlags([]string{"-project", projectID.String()})
	require.NoError(t, err)
	require.Equal(t, "postgres://localhost/gram", cfg.dbURL)
	require.Equal(t, projectID, cfg.projectID)
	require.Equal(t, uuid.Nil, cfg.cursor)
	require.Equal(t, int32(defaultBatchSize), cfg.batchSize)
	require.True(t, cfg.dryRun)
}

func TestParseFlagsRequiresProject(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://localhost/gram")

	_, err := parseFlags(nil)
	require.ErrorContains(t, err, "invalid -project")
}

func TestExecuteDryRunOnlyReports(t *testing.T) {
	t.Parallel()

	worker := &fakeBackfiller{
		statuses: []aiintegrations.SyncScheduleBackfillStatus{{
			PrimarySyncsPending:   3,
			AnalyticsSyncsPending: 2,
		}},
		batches: nil,
	}
	var out bytes.Buffer

	err := execute(t.Context(), &out, worker, config{
		dbURL:     "unused",
		projectID: uuid.MustParse("019b1000-0000-7000-8000-000000000001"),
		cursor:    uuid.Nil,
		batchSize: 10,
		dryRun:    true,
	})
	require.NoError(t, err)
	require.Zero(t, worker.batchCalls)
	require.Contains(t, out.String(), "before: primary_pending=3 analytics_pending=2")
	require.Contains(t, out.String(), "DRY RUN")
}

func TestExecuteAppliesResumableBatches(t *testing.T) {
	t.Parallel()

	firstCursor := uuid.MustParse("019b1000-0000-7000-8000-000000000001")
	lastCursor := uuid.MustParse("019b1000-0000-7000-8000-000000000002")
	worker := &fakeBackfiller{
		statuses: []aiintegrations.SyncScheduleBackfillStatus{
			{PrimarySyncsPending: 2, AnalyticsSyncsPending: 2},
			{PrimarySyncsPending: 0, AnalyticsSyncsPending: 0},
		},
		batches: []aiintegrations.SyncScheduleBackfillBatch{
			{
				ConfigsProcessed:          1,
				PrimarySyncsUpdated:       1,
				AnalyticsSchedulesCreated: 2,
				LastConfigID:              firstCursor,
			},
			{
				ConfigsProcessed:          1,
				PrimarySyncsUpdated:       1,
				AnalyticsSchedulesCreated: 0,
				LastConfigID:              lastCursor,
			},
			{
				ConfigsProcessed:          0,
				PrimarySyncsUpdated:       0,
				AnalyticsSchedulesCreated: 0,
				LastConfigID:              uuid.Nil,
			},
		},
	}
	var out bytes.Buffer

	err := execute(t.Context(), &out, worker, config{
		dbURL:     "unused",
		projectID: uuid.MustParse("019b1000-0000-7000-8000-000000000003"),
		cursor:    uuid.Nil,
		batchSize: 1,
		dryRun:    false,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{uuid.Nil, firstCursor, lastCursor}, worker.after)
	require.Contains(t, out.String(), "applied: configs=2 primary_updated=2 analytics_created=2")
	require.Contains(t, out.String(), "after: primary_pending=0 analytics_pending=0")
}

func TestExecuteRejectsIncompleteBackfill(t *testing.T) {
	t.Parallel()

	worker := &fakeBackfiller{
		statuses: []aiintegrations.SyncScheduleBackfillStatus{
			{PrimarySyncsPending: 1, AnalyticsSyncsPending: 1},
			{PrimarySyncsPending: 1, AnalyticsSyncsPending: 1},
		},
		batches: []aiintegrations.SyncScheduleBackfillBatch{{
			ConfigsProcessed:          0,
			PrimarySyncsUpdated:       0,
			AnalyticsSchedulesCreated: 0,
			LastConfigID:              uuid.Nil,
		}},
	}

	err := execute(t.Context(), &bytes.Buffer{}, worker, config{
		dbURL:     "unused",
		projectID: uuid.MustParse("019b1000-0000-7000-8000-000000000001"),
		cursor:    uuid.Nil,
		batchSize: 10,
		dryRun:    false,
	})
	require.ErrorContains(t, err, "backfill incomplete")
}

type fakeBackfiller struct {
	statuses    []aiintegrations.SyncScheduleBackfillStatus
	batches     []aiintegrations.SyncScheduleBackfillBatch
	statusCalls int
	batchCalls  int
	after       []uuid.UUID
}

func (f *fakeBackfiller) Status(context.Context) (aiintegrations.SyncScheduleBackfillStatus, error) {
	status := f.statuses[f.statusCalls]
	f.statusCalls++
	return status, nil
}

func (f *fakeBackfiller) BackfillBatch(_ context.Context, after uuid.UUID, _ int32) (aiintegrations.SyncScheduleBackfillBatch, error) {
	f.after = append(f.after, after)
	batch := f.batches[f.batchCalls]
	f.batchCalls++
	return batch, nil
}
