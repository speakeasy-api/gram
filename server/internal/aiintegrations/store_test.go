package aiintegrations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
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
		schedules[row.Schedule] = row.Kind
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
