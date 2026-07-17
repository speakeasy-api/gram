package aiintegrations

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestUpsertWithTxCreatesConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	result := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)
	require.True(t, result.CreatedNewGeneration)
	require.NotNil(t, result.Row)
	require.Equal(t, result.Row.ID, result.Config.ID)
	require.Equal(t, watermark.UTC().Add(pollIntervalForSchedule(ScheduleCursor)), result.Config.NextPollAfter)
	require.Equal(t, watermark, result.Config.PollWatermarkAt)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxSettingsUpdateKeepsConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)

	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "", false, false, nil, nil)
	require.False(t, updated.CreatedNewGeneration)
	require.Equal(t, created.Config.ID, updated.Config.ID)
	require.False(t, updated.Config.Enabled)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func TestUpsertWithTxKeyReplacementCreatesNewConfigGeneration(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)

	replacedWatermark := time.Now().UTC().Add(-initialUsagePollLookback)
	replaced := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "new-cursor-key", true, true, nil, &replacedWatermark)
	require.True(t, replaced.CreatedNewGeneration)
	require.NotEqual(t, created.Config.ID, replaced.Config.ID)

	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
	require.Equal(t, int64(2), countAIIntegrationConfigs(t, ctx, conn, orgID, true))
}

func TestUpsertWithTxStartsAllProviderSchedules(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     SyncKindCursor,
		ScheduleAnthropicAnalyticsUsage: SyncKindTime,
		ScheduleAnthropicAnalyticsCost:  SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, created.Config.ID))
}

func TestUpsertWithTxStartsSingleCursorSchedule(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)

	require.Equal(t, map[string]string{
		ScheduleCursor: SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, created.Config.ID))
}

func TestUpsertWithTxFillsExistingPrimarySyncDiscriminators(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)
	original := getPrimarySyncDiscriminators(t, ctx, conn, created.Config)
	require.NoError(t, repo.New(conn).ClearSyncScheduleDiscriminatorsForTest(ctx, repo.ClearSyncScheduleDiscriminatorsForTestParams{
		AiIntegrationConfigID: created.Config.ID,
		ProjectID:             created.Config.ProjectID,
	}))

	loaded, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderCursor)
	require.NoError(t, err)
	require.Equal(t, created.Config.ID, loaded.ID)

	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "", false, false, nil, nil)
	require.Equal(t, created.Config.ID, updated.Config.ID)

	syncRow := getPrimarySyncDiscriminators(t, ctx, conn, updated.Config)
	require.Equal(t, original.ID, syncRow.ID)
	require.Equal(t, ProviderCursor, syncRow.Schedule.String)
	require.Equal(t, SyncKindTime, syncRow.Kind.String)
	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, updated.Config))
}

func TestEnsurePrimarySyncHandlesConcurrentFirstWriters(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)
	require.NoError(t, repo.New(conn).DeleteSyncRowsForTest(ctx, repo.DeleteSyncRowsForTestParams{
		AiIntegrationConfigID: created.Config.ID,
		ProjectID:             created.Config.ProjectID,
	}))

	const writers = 8
	start := make(chan struct{})
	errs := make(chan error, writers)
	ids := make(chan uuid.UUID, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			<-start
			row, err := store.ensurePrimarySync(ctx, conn, created.Config.ID, created.Config.ProjectID)
			errs <- err
			ids <- row.ID
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)

	for err := range errs {
		require.NoError(t, err)
	}
	var syncID uuid.UUID
	for id := range ids {
		require.NotEqual(t, uuid.Nil, id)
		if syncID == uuid.Nil {
			syncID = id
		}
		require.Equal(t, syncID, id)
	}
	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, created.Config))
}

func TestListUsagePollCandidatesReturnsEveryDueSchedule(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	candidates, err := store.ListUsagePollCandidates(ctx, time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	schedules := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		require.Equal(t, created.Config.ID, candidate.ID)
		require.Equal(t, ProviderAnthropicCompliance, candidate.Provider)
		schedules[candidate.Schedule] = candidate.Kind
	}
	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     SyncKindCursor,
		ScheduleAnthropicAnalyticsUsage: SyncKindTime,
		ScheduleAnthropicAnalyticsCost:  SyncKindTime,
	}, schedules)
}

func TestBackfillSyncSchedulesIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_backfill"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)
	queries := repo.New(conn)
	require.NoError(t, queries.DeleteSecondarySyncSchedulesForTest(ctx, repo.DeleteSecondarySyncSchedulesForTestParams{
		AiIntegrationConfigID: created.Config.ID,
		ProjectID:             created.Config.ProjectID,
		PrimarySchedule:       conv.ToPGText(ScheduleAnthropicCompliance),
	}))
	require.NoError(t, queries.ClearSyncScheduleDiscriminatorsForTest(ctx, repo.ClearSyncScheduleDiscriminatorsForTestParams{
		AiIntegrationConfigID: created.Config.ID,
		ProjectID:             created.Config.ProjectID,
	}))

	result, err := queries.BackfillSyncSchedules(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.PrimarySyncsUpdated)
	require.Equal(t, int64(2), result.AnalyticsSchedulesCreated)
	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     SyncKindCursor,
		ScheduleAnthropicAnalyticsUsage: SyncKindTime,
		ScheduleAnthropicAnalyticsCost:  SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, created.Config.ID))

	rerun, err := queries.BackfillSyncSchedules(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), rerun.PrimarySyncsUpdated)
	require.Equal(t, int64(0), rerun.AnalyticsSchedulesCreated)
}

func TestRecordUsagePollFailureStoresErrorAsData(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, &watermark)

	message := `cursor rejected the configured api key'; DROP TABLE ai_integration_syncs; -- <script>alert(1)</script>`
	require.NoError(t, store.RecordUsagePollFailure(ctx, created.Config.ID, ProviderCursor, time.Now(), errors.New(message)))

	cfg, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderCursor)
	require.NoError(t, err)
	require.Equal(t, message, cfg.LastPollError)
	require.Equal(t, int64(1), countAIIntegrationConfigs(t, ctx, conn, orgID, false))
}

func upsertConfigWithTx(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	store *Store,
	orgID string,
	provider string,
	apiKey string,
	apiKeySupplied bool,
	enabled bool,
	externalOrganizationID *string,
	resetPollWatermarkAt *time.Time,
) UpsertResult {
	t.Helper()

	var result UpsertResult
	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		result, err = store.upsertWithTx(ctx, tx, orgID, provider, apiKey, apiKeySupplied, enabled, externalOrganizationID, nil, resetPollWatermarkAt)
		return err
	}))
	return result
}

func listSyncSchedules(t *testing.T, ctx context.Context, conn *pgxpool.Pool, configID uuid.UUID) map[string]string {
	t.Helper()

	rows, err := repo.New(conn).ListSyncSchedules(ctx, configID)
	require.NoError(t, err)

	schedules := map[string]string{}
	for _, row := range rows {
		schedules[row.Schedule.String] = row.Kind.String
	}
	return schedules
}

func countAIIntegrationConfigs(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, includeDeleted bool) int64 {
	t.Helper()

	count, err := repo.New(conn).CountConfigsByOrganization(ctx, repo.CountConfigsByOrganizationParams{
		OrganizationID: orgID,
		IncludeDeleted: includeDeleted,
	})
	require.NoError(t, err)
	return count
}

func getPrimarySyncDiscriminators(t *testing.T, ctx context.Context, conn *pgxpool.Pool, cfg Config) repo.GetPrimarySyncDiscriminatorsForTestRow {
	t.Helper()

	row, err := repo.New(conn).GetPrimarySyncDiscriminatorsForTest(ctx, repo.GetPrimarySyncDiscriminatorsForTestParams{
		AiIntegrationConfigID: cfg.ID,
		ProjectID:             cfg.ProjectID,
	})
	require.NoError(t, err)
	return row
}

func countSyncRows(t *testing.T, ctx context.Context, conn *pgxpool.Pool, cfg Config) int64 {
	t.Helper()

	count, err := repo.New(conn).CountSyncRowsForTest(ctx, repo.CountSyncRowsForTestParams{
		AiIntegrationConfigID: cfg.ID,
		ProjectID:             cfg.ProjectID,
	})
	require.NoError(t, err)
	return count
}
