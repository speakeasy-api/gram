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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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

func TestRecordUsagePollFailureStoresOnlyCustomerMessage(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	watermark := time.Now().UTC().Add(-initialUsagePollLookback)
	externalOrgID := "org-openai"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCodexCompliance, "codex-key", true, true, &externalOrgID, &watermark)
	shareableErr := oops.E(
		oops.CodeUnexpected,
		errors.New(`provider payload contains "user@example.com"`),
		"sync codex cost data",
	)

	require.NoError(t, store.RecordUsagePollFailure(ctx, created.Config.ID, ProviderCodexCompliance, time.Now(), shareableErr))

	cfg, _, err := store.loadForOrgAndProviderRow(ctx, orgID, ProviderCodexCompliance)
	require.NoError(t, err)
	require.Equal(t, "sync codex cost data", cfg.LastPollError)
	require.NotContains(t, cfg.LastPollError, "user@example.com")
}

func TestInitialPollLookbackForProviderUsesLongCodexBackfill(t *testing.T) {
	t.Parallel()

	// Codex compliance backfills a month of cheap hourly aggregates; other
	// providers keep the conservative 24h first-poll window.
	require.Equal(t, 30*24*time.Hour, initialPollLookbackForProvider(ProviderCodexCompliance))
	require.Equal(t, 24*time.Hour, initialPollLookbackForProvider(ProviderCursor))
	require.Equal(t, 24*time.Hour, initialPollLookbackForProvider(ProviderAnthropicCompliance))
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

func TestRecordSchedulePollFailureBacksOffWithFailureStreak(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	interval := pollIntervalForSchedule(ScheduleAnthropicCompliance)
	cause := errors.New("list anthropic compliance activities: 503 Service Unavailable")

	// Failure k reschedules the next poll interval * 2^(k-1) out, capped at
	// 2^pollFailureMaxBackoffDoublings: the first failure keeps the base
	// cadence, each repeat doubles the delay, and a chronic failure settles
	// at the cap instead of retrying at full cadence forever.
	for k := 1; k <= pollFailureMaxBackoffDoublings+2; k++ {
		callTime := time.Now().UTC()
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, callTime, cause, 0))

		expected := callTime.Add(interval * time.Duration(1<<min(k-1, pollFailureMaxBackoffDoublings)))
		state := findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicCompliance)
		require.WithinDuration(t, expected, state.NextPollAfter, 5*time.Second)
	}

	// A success resets the streak, so the next failure is back to the base
	// cadence.
	require.NoError(t, store.RecordSchedulePollSuccess(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now().UTC()))
	callTime := time.Now().UTC()
	require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, callTime, cause, 0))
	state := findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicCompliance)
	require.WithinDuration(t, callTime.Add(interval), state.NextPollAfter, 5*time.Second)
}

func TestRecordSchedulePollFailureBackoffRespectsCeilingAndNowAnchor(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("analytics export unavailable")

	// The 4h analytics schedule hits the absolute ceiling on its second
	// consecutive failure: doubling alone would reach 8h and, at the streak
	// cap, over ten days.
	interval := pollIntervalForSchedule(ScheduleAnthropicAnalyticsUsage)
	require.Greater(t, 2*interval, pollFailureBackoffCeiling)

	callTime := time.Now().UTC()
	require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicAnalyticsUsage, callTime, cause, 0))
	state := findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicAnalyticsUsage)
	require.WithinDuration(t, callTime.Add(interval), state.NextPollAfter, 5*time.Second)

	callTime = time.Now().UTC()
	require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicAnalyticsUsage, callTime, cause, 0))
	state = findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicAnalyticsUsage)
	require.WithinDuration(t, callTime.Add(pollFailureBackoffCeiling), state.NextPollAfter, 5*time.Second)

	// A poll whose endTime is far in the past (a long failing run) anchors
	// on now instead, so the backoff is not erased by landing in the past.
	stale := time.Now().UTC().Add(-2 * time.Hour)
	callTime = time.Now().UTC()
	require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicAnalyticsUsage, stale, cause, 0))
	state = findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicAnalyticsUsage)
	require.WithinDuration(t, callTime.Add(pollFailureBackoffCeiling), state.NextPollAfter, 5*time.Second)
}

func TestClearSyncSchedulePausesMakesFailingSchedulesDue(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("compliance feed unavailable")
	for range 3 {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now().UTC(), cause, 0))
	}
	backedOff := findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicCompliance)
	require.Greater(t, backedOff.NextPollAfter, time.Now().Add(2*pollIntervalForSchedule(ScheduleAnthropicCompliance)))

	// A settings-only save clears the streak and must also pull the
	// backed-off next poll in, or the just-fixed integration stays dark
	// until the backed-off time arrives.
	upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "", false, true, &extOrgID, nil)

	state := findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicCompliance)
	require.Equal(t, int32(0), state.ConsecutiveFailures)
	require.LessOrEqual(t, state.NextPollAfter, time.Now().Add(time.Second))
}

func TestSetSyncScheduleDisabledReenableMakesFailingScheduleDue(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("compliance feed unavailable")
	for range 3 {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now().UTC(), cause, 0))
	}

	setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicCompliance, true)
	reenabled := setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicCompliance, false)

	// Re-enabling a schedule with a failure streak pulls its backed-off next
	// poll in so it is picked up on the next scheduler tick, and resets the
	// streak so the fresh run polls at full cadence instead of continuing the
	// old backoff toward the auto-pause threshold.
	require.LessOrEqual(t, reenabled.NextPollAfter, time.Now().Add(time.Second))
	require.Equal(t, int32(0), reenabled.ConsecutiveFailures)
}

func TestSetSyncScheduleDisabledAlreadyEnabledKeepsFailureBackoff(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("compliance feed unavailable")
	for range 3 {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now().UTC(), cause, 0))
	}
	backedOff := findSyncSchedule(t, ctx, store, created.Config.ID, ScheduleAnthropicCompliance)

	stillEnabled := setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicCompliance, false)

	require.Equal(t, backedOff.NextPollAfter, stillEnabled.NextPollAfter)
	require.Equal(t, backedOff.ConsecutiveFailures, stillEnabled.ConsecutiveFailures)
}

func findSyncSchedule(t *testing.T, ctx context.Context, store *Store, configID uuid.UUID, schedule string) SyncSchedule {
	t.Helper()

	schedules, err := store.ListSyncSchedules(ctx, configID)
	require.NoError(t, err)
	for _, state := range schedules {
		if state.Schedule == schedule {
			return state
		}
	}
	t.Fatalf("schedule %s not found on config %s", schedule, configID)
	panic("unreachable")
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

func TestUpsertRejectsNonUUIDWorkspaceIDForChatGPTCompliance(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	// The chatgpt_compliance scope id is a ChatGPT workspace UUID; an org-…
	// API organization id (valid for codex_compliance) must be rejected at
	// upsert rather than failing every background poll later.
	orgStyleID := "org-123abc"
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.upsertWithTx(ctx, tx, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &orgStyleID, nil, nil)
		return err
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ChatGPT workspace UUID")
}

// TestUpsertWithTxStartsChatGPTSchedules: the chatgpt_compliance config owns
// BOTH workspace-scoped feeds — conversation messages and Codex cloud task
// transcripts — so saving it must start both time-kind schedules. This is
// also the wiring ensureActiveSyncSchedules reconciles onto existing configs.
func TestUpsertWithTxStartsChatGPTSchedules(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &workspaceID, nil)

	require.Equal(t, map[string]string{
		ScheduleChatGPTCompliance:  SyncKindTime,
		ScheduleCodexCloudSessions: SyncKindTime,
	}, listSyncSchedules(t, ctx, conn, created.Config.ID))
}

// TestUpsertResetsAllProviderScheduleWatermarks: a key or external-scope
// change resets every SYNCED schedule on the config, not just the
// provider-named one — all of a provider's feeds key on the same
// credentials/scope, and a sibling left on the old scope's watermark would
// silently skip the new scope's history (e.g. a ChatGPT workspace-id fix
// re-backfilling conversations but not Codex cloud transcripts). A
// never-synced sibling keeps its epoch sentinel: that state drives first-sync
// behavior (initial lookback, finality probing) and must not be erased.
func TestUpsertResetsAllProviderScheduleWatermarks(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	workspaceID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", true, true, &workspaceID, nil)

	// The transcript sibling has synced: its stale watermark must reset.
	syncedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	siblingCfg, err := store.GetUsagePollConfig(ctx, created.Config.ID, ScheduleCodexCloudSessions)
	require.NoError(t, err)
	require.NoError(t, store.AdvanceWatermark(ctx, siblingCfg.SyncID, timewindowpoller.CompletedCheckpoint(syncedAt)))

	resetAt := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	otherWorkspace := "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderChatGPTCompliance, "chatgpt-key", false, true, &otherWorkspace, &resetAt)

	rows, err := repo.New(conn).ListSyncSchedules(ctx, created.Config.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, resetAt.Add(chatgptCompliancePollInterval), row.NextPollAfter.Time.UTC(), row.Schedule)
	}
	siblingCfg, err = store.GetUsagePollConfig(ctx, created.Config.ID, ScheduleCodexCloudSessions)
	require.NoError(t, err)
	require.Equal(t, resetAt, siblingCfg.PollWatermarkAt.UTC())

	// A fresh config's never-synced sibling keeps its epoch sentinel through
	// a reset (only the provider-named schedule always resets).
	orgB := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgB,
		Name:        "Reset Fresh Org",
		Slug:        orgB,
		WorkosID:    pgtype.Text{String: orgB, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	_, err = projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Reset Fresh Project",
		Slug:           "project-" + uuid.NewString()[:8],
		OrganizationID: orgB,
	})
	require.NoError(t, err)
	freshWorkspace := "cccccccc-dddd-4eee-8fff-000000000000"
	fresh := upsertConfigWithTx(t, ctx, conn, store, orgB, ProviderChatGPTCompliance, "chatgpt-key", true, true, &freshWorkspace, &resetAt)
	freshRows, err := repo.New(conn).ListSyncSchedules(ctx, fresh.Config.ID)
	require.NoError(t, err)
	require.Len(t, freshRows, 2)
	for _, row := range freshRows {
		if row.Schedule == ScheduleCodexCloudSessions {
			require.Equal(t, time.Unix(0, 0).UTC(), row.NextPollAfter.Time.UTC(), "never-synced sibling stays due-immediately at epoch")
		}
	}
}
