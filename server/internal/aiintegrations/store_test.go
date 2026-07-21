package aiintegrations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
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

func TestListUsagePollCandidatesReturnsEveryDueSchedule(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	candidates, err := store.ListUsagePollCandidates(ctx, time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	schedules := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		require.NotEqual(t, uuid.Nil, candidate.SyncID)
		require.Equal(t, ProviderAnthropicCompliance, candidate.Provider)
		schedules[candidate.Schedule] = candidate.Kind
	}
	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     SyncKindCursor,
		ScheduleAnthropicAnalyticsUsage: SyncKindTime,
		ScheduleAnthropicAnalyticsCost:  SyncKindTime,
	}, schedules)
}
func TestListUsagePollCandidatesAdoptsLegacyRowsAndCreatesSchedules(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	configID := insertLegacyConfig(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, true)

	candidates, err := store.ListUsagePollCandidates(ctx, time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 3)
	for _, candidate := range candidates {
		require.NotEqual(t, uuid.Nil, candidate.SyncID)
		require.Equal(t, ProviderAnthropicCompliance, candidate.Provider)
	}

	// The pre-schedule row was adopted as the provider-named schedule (not
	// duplicated) and the analytics schedules were created alongside it.
	require.Equal(t, int64(3), countSyncRows(t, ctx, conn, configID))
	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     SyncKindCursor,
		ScheduleAnthropicAnalyticsUsage: SyncKindTime,
		ScheduleAnthropicAnalyticsCost:  SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, configID))
}

func TestGetUsagePollConfigBySyncIDFallsBackForLegacySyncRow(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	configID := insertLegacyConfig(t, ctx, conn, store, orgID, ProviderCursor, true)
	rows, err := repo.New(conn).ListSyncSchedules(ctx, configID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, store.AdvanceWatermark(ctx, rows[0].ID, timewindowpoller.CompletedCheckpoint(epochTime())))

	cfg, schedule, err := store.GetUsagePollConfigBySyncID(ctx, rows[0].ID)
	require.NoError(t, err)

	require.Equal(t, rows[0].ID, cfg.SyncID)
	require.Equal(t, ScheduleCursor, schedule)
	require.True(t, cfg.PollWatermarkAt.IsZero())
	require.True(t, cfg.PollCheckpoint.Watermark.IsZero())
}

func TestListUsagePollCandidatesIgnoresInactiveConfigs(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	configID := insertLegacyConfig(t, ctx, conn, store, orgID, ProviderCursor, false)

	candidates, err := store.ListUsagePollCandidates(ctx, time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Empty(t, candidates)

	// The disabled config's pre-schedule row is left untouched: no adoption,
	// no new schedule rows. This process is forward-looking, not a backfill.
	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, configID))
	require.Equal(t, map[string]string{"": ""}, listSyncSchedules(t, ctx, conn, configID))
}

func TestUpsertWithTxAdoptsLegacySyncRow(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	configID := insertLegacyConfig(t, ctx, conn, store, orgID, ProviderCursor, true)

	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "", false, true, nil, nil)
	require.Equal(t, configID, updated.Config.ID)

	require.Equal(t, int64(1), countSyncRows(t, ctx, conn, configID))
	require.Equal(t, map[string]string{
		ScheduleCursor: SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, configID))
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

// insertLegacyConfig writes a config plus a pre-schedule sync row (NULL
// schedule/kind), simulating state left behind by code that predates the
// schedule columns.
func insertLegacyConfig(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, orgID string, provider string, enabled bool) uuid.UUID {
	t.Helper()

	q := repo.New(conn)
	projectID, err := q.GetFirstProjectByOrganization(ctx, orgID)
	require.NoError(t, err)

	encryptedKey, err := store.encryptAPIKey("legacy-key")
	require.NoError(t, err)

	row, err := q.InsertConfig(ctx, repo.InsertConfigParams{
		OrganizationID:         orgID,
		Provider:               provider,
		ProjectID:              projectID,
		ExternalOrganizationID: pgtype.Text{String: "", Valid: false},
		ApiKeyEncrypted:        encryptedKey,
		Enabled:                enabled,
		BillingMode:            pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	require.NoError(t, q.InsertPreScheduleSyncRowForTest(ctx, row.ID))

	return row.ID
}

func countSyncRows(t *testing.T, ctx context.Context, conn *pgxpool.Pool, configID uuid.UUID) int64 {
	t.Helper()

	count, err := repo.New(conn).CountSyncRowsForTest(ctx, configID)
	require.NoError(t, err)
	return count
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
