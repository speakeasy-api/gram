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
func TestListUsagePollCandidatesCreatesMissingSchedules(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	configID := insertConfigWithProviderScheduleOnly(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, true)

	candidates, err := store.ListUsagePollCandidates(ctx, time.Now().Add(time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 3)
	for _, candidate := range candidates {
		require.NotEqual(t, uuid.Nil, candidate.SyncID)
		require.Equal(t, ProviderAnthropicCompliance, candidate.Provider)
	}

	// The analytics schedules the config was missing were created alongside
	// the existing provider-named row.
	require.Equal(t, int64(3), countSyncRows(t, ctx, conn, configID))
	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     SyncKindCursor,
		ScheduleAnthropicAnalyticsUsage: SyncKindTime,
		ScheduleAnthropicAnalyticsCost:  SyncKindTime,
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

func TestUpsertWithTxStartsSingleCodexSchedule(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	externalOrgID := "org-openai"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCodexCompliance, "codex-key", true, true, &externalOrgID, nil)

	require.Equal(t, map[string]string{
		ScheduleCodexCompliance: SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, created.Config.ID))
}

func TestUpsertWithTxRequiresCodexOrganizationID(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	workspaceID := "75179082-77da-4127-8031-fce17dddb623"
	require.Error(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.upsertWithTx(ctx, tx, orgID, ProviderCodexCompliance, "codex-key", true, true, &workspaceID, nil, nil)
		return err
	}))
}

func TestUpsertWithTxRejectsCodexPathLikeOrganizationID(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	externalOrgID := "org-openai/../other"
	require.Error(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.upsertWithTx(ctx, tx, orgID, ProviderCodexCompliance, "codex-key", true, true, &externalOrgID, nil, nil)
		return err
	}))
}

func TestRecordSchedulePollFailureAutoPausesAfterThreshold(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("anthropic compliance organization not found or compliance api access not enabled")
	for range AutoPauseAfterRejectedPolls - 1 {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now(), cause, AutoPauseAfterRejectedPolls))
	}

	// One failure short of the threshold the schedule is still polled.
	candidates := listCandidateSchedules(t, ctx, store)
	require.Contains(t, candidates, ScheduleAnthropicCompliance)

	require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now(), cause, AutoPauseAfterRejectedPolls))

	// The paused schedule disappears from candidate selection while the
	// config's other schedules keep polling.
	candidates = listCandidateSchedules(t, ctx, store)
	require.NotContains(t, candidates, ScheduleAnthropicCompliance)
	require.Contains(t, candidates, ScheduleAnthropicAnalyticsUsage)
	require.Contains(t, candidates, ScheduleAnthropicAnalyticsCost)
}

func TestRecordSchedulePollFailureWithoutPauseThresholdNeverPauses(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	// Retryable failures pass a zero threshold and must never pause, no
	// matter how long the streak gets.
	for range AutoPauseAfterRejectedPolls + 2 {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now(), errors.New("transient provider error"), 0))
	}

	require.Contains(t, listCandidateSchedules(t, ctx, store), ScheduleAnthropicCompliance)
}

func TestUpsertWithTxClearsAutoPause(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("anthropic compliance rejected the configured api key")
	for range AutoPauseAfterRejectedPolls {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now(), cause, AutoPauseAfterRejectedPolls))
	}
	require.NotContains(t, listCandidateSchedules(t, ctx, store), ScheduleAnthropicCompliance)

	// Saving the integration (settings-only update, same config generation)
	// lifts the pause and resets the failure streak.
	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "", false, true, &extOrgID, nil)
	require.Equal(t, created.Config.ID, updated.Config.ID)

	require.Contains(t, listCandidateSchedules(t, ctx, store), ScheduleAnthropicCompliance)

	cfg, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderAnthropicCompliance)
	require.NoError(t, err)
	require.Equal(t, int32(0), cfg.ConsecutiveFailures)
}

// listCandidateSchedules returns the schedules of every currently due poll
// candidate, keyed for membership assertions.
func listCandidateSchedules(t *testing.T, ctx context.Context, store *Store) map[string]bool {
	t.Helper()

	candidates, err := store.ListUsagePollCandidates(ctx, time.Now().Add(24*time.Hour), 100)
	require.NoError(t, err)

	schedules := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		schedules[candidate.Schedule] = true
	}
	return schedules
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

// insertConfigWithProviderScheduleOnly writes a config plus only its
// provider-named sync row, simulating a config that predates additional
// provider schedules.
func insertConfigWithProviderScheduleOnly(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, orgID string, provider string, enabled bool) uuid.UUID {
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

	providerSched := providerSyncSchedule(provider)
	initialAt := time.Now().UTC()
	if providerSched.kind == SyncKindTime {
		initialAt = epochTime()
	}
	_, err = q.EnsureSync(ctx, repo.EnsureSyncParams{
		AiIntegrationConfigID: row.ID,
		Schedule:              providerSched.schedule,
		Kind:                  providerSched.kind,
		PollWatermarkAt:       conv.ToPGTimestamptz(initialAt),
		NextPollAfter:         conv.ToPGTimestamptz(initialAt),
	})
	require.NoError(t, err)

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
