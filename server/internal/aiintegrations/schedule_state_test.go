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
)

func TestSetSyncScheduleDisabledExcludesFromCandidates(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	disabled := setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicAnalyticsUsage, true)
	require.False(t, disabled.DisabledAt.IsZero())

	// The disabled schedule drops out of candidate selection; the config's
	// other schedules keep polling.
	candidates := listCandidateSchedules(t, ctx, store)
	require.NotContains(t, candidates, ScheduleAnthropicAnalyticsUsage)
	require.Contains(t, candidates, ScheduleAnthropicCompliance)
	require.Contains(t, candidates, ScheduleAnthropicAnalyticsCost)

	enabled := setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicAnalyticsUsage, false)
	require.True(t, enabled.DisabledAt.IsZero())

	// Its stale next_poll_after is already due, so re-enabling makes it a
	// candidate again without any extra scheduling work.
	require.Contains(t, listCandidateSchedules(t, ctx, store), ScheduleAnthropicAnalyticsUsage)
}

func TestUpsertWithTxKeepsUserDisabledSchedule(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicAnalyticsCost, true)

	// A settings-only save clears automatic pauses but must not override the
	// user's explicit pause.
	updated := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "", false, true, &extOrgID, nil)
	require.Equal(t, created.Config.ID, updated.Config.ID)

	candidates := listCandidateSchedules(t, ctx, store)
	require.NotContains(t, candidates, ScheduleAnthropicAnalyticsCost)
	require.Contains(t, candidates, ScheduleAnthropicCompliance)
}

func TestRetrySyncScheduleLiftsAutoPause(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	cause := errors.New("anthropic compliance rejected the configured api key")
	for range AutoPauseAfterRejectedPolls {
		require.NoError(t, store.RecordSchedulePollFailure(ctx, created.Config.ID, ScheduleAnthropicCompliance, time.Now().Add(time.Hour), cause, AutoPauseAfterRejectedPolls))
	}
	require.NotContains(t, listCandidateSchedules(t, ctx, store), ScheduleAnthropicCompliance)

	var retried SyncSchedule
	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		retried, err = store.retrySyncScheduleWithTx(ctx, tx, created.Config.ID, ScheduleAnthropicCompliance)
		return err
	}))
	require.True(t, retried.AutoPausedAt.IsZero())
	require.Equal(t, int32(0), retried.ConsecutiveFailures)
	// The stored error clears on retry; a failing poll re-records it.
	require.Empty(t, retried.LastPollError)
	require.True(t, retried.LastPollFailedAt.IsZero())

	// The retried schedule is due immediately, so the next scheduler tick
	// picks it up.
	require.Contains(t, listCandidateSchedules(t, ctx, store), ScheduleAnthropicCompliance)
}

func TestRetrySyncScheduleUnknownScheduleReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderCursor, "cursor-key", true, true, nil, nil)

	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.retrySyncScheduleWithTx(ctx, tx, created.Config.ID, "no_such_schedule")
		return err
	})
	require.Error(t, err)
}

func TestStartUsagePollSkipsDisabledSchedules(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	extOrgID := "org_ext_1"
	created := upsertConfigWithTx(t, ctx, conn, store, orgID, ProviderAnthropicCompliance, "anthropic-key", true, true, &extOrgID, nil)

	setScheduleDisabled(t, ctx, conn, store, created.Config.ID, ScheduleAnthropicAnalyticsUsage, true)

	schedules, err := store.ListSyncSchedules(ctx, created.Config.ID)
	require.NoError(t, err)
	syncIDsBySchedule := make(map[string]uuid.UUID, len(schedules))
	for _, schedule := range schedules {
		syncIDsBySchedule[schedule.Schedule] = schedule.ID
	}

	poller := &fakeConfigPoller{}
	svc := &Service{store: store, configPoller: poller}

	require.NoError(t, svc.startUsagePoll(ctx, "acme", created.Config.ID, ProviderAnthropicCompliance))
	require.Equal(t, []fakeConfigPollerCall{
		{organizationSlug: "acme", syncID: syncIDsBySchedule[ScheduleAnthropicCompliance]},
		{organizationSlug: "acme", syncID: syncIDsBySchedule[ScheduleAnthropicAnalyticsCost]},
	}, poller.calls)
}

func TestDeriveScheduleStatusPrecedence(t *testing.T) {
	t.Parallel()

	now := time.Now()

	require.Equal(t, "pending", deriveScheduleStatus(SyncSchedule{}))
	require.Equal(t, "success", deriveScheduleStatus(SyncSchedule{LastPollSuccessAt: now}))
	require.Equal(t, "failed", deriveScheduleStatus(SyncSchedule{LastPollSuccessAt: now, LastPollError: "boom", LastPollFailedAt: now}))
	// Auto-pause outranks a plain failure; a user pause outranks everything.
	require.Equal(t, "auto_paused", deriveScheduleStatus(SyncSchedule{LastPollError: "boom", LastPollFailedAt: now, AutoPausedAt: now}))
	require.Equal(t, "disabled", deriveScheduleStatus(SyncSchedule{LastPollError: "boom", AutoPausedAt: now, DisabledAt: now}))
}

func TestScheduleViewMapsEnabledAndTimestamps(t *testing.T) {
	t.Parallel()

	success := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	next := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)

	view := scheduleView(SyncSchedule{
		ID:                uuid.New(),
		Schedule:          ScheduleAnthropicAnalyticsUsage,
		Kind:              SyncKindTime,
		LastPollSuccessAt: success,
		NextPollAfter:     next,
	})
	require.True(t, view.Enabled)
	require.Equal(t, "success", view.Status)
	require.NotNil(t, view.Stream)
	require.Equal(t, StreamClaudeChatUsageTokens, *view.Stream)
	require.NotNil(t, view.StreamKind)
	require.Equal(t, StreamKindMetrics, *view.StreamKind)
	require.NotNil(t, view.LastPollSuccessAt)
	require.Equal(t, "2026-07-20T10:00:00Z", *view.LastPollSuccessAt)
	require.NotNil(t, view.NextPollAfter)
	require.Equal(t, "2026-07-20T11:00:00Z", *view.NextPollAfter)
	require.Nil(t, view.LastPollError)
	require.Nil(t, view.AutoPausedAt)

	disabledView := scheduleView(SyncSchedule{
		ID:         uuid.New(),
		Schedule:   ScheduleAnthropicAnalyticsUsage,
		Kind:       SyncKindTime,
		DisabledAt: time.Now(),
	})
	require.False(t, disabledView.Enabled)
	require.Equal(t, "disabled", disabledView.Status)
}

func TestScheduleBelongsToProvider(t *testing.T) {
	t.Parallel()

	require.True(t, scheduleBelongsToProvider(ProviderAnthropicCompliance, ScheduleAnthropicAnalyticsUsage))
	require.True(t, scheduleBelongsToProvider(ProviderCursor, ScheduleCursor))
	require.False(t, scheduleBelongsToProvider(ProviderCursor, ScheduleAnthropicAnalyticsUsage))
	require.False(t, scheduleBelongsToProvider(ProviderAnthropicCompliance, "made_up"))
}

func TestStreamForScheduleCoversEveryRegisteredSchedule(t *testing.T) {
	t.Parallel()

	// Every schedule any provider runs must have a registered stream —
	// adding a schedule without one is a bug this test catches.
	for _, provider := range []string{ProviderCursor, ProviderAnthropicCompliance, ProviderCodexCompliance} {
		for _, sched := range syncSchedulesFor(provider) {
			stream := streamForSchedule(sched.schedule)
			require.NotEmpty(t, stream.name, "schedule %s has no stream name", sched.schedule)
			require.Contains(t, []string{StreamKindEvents, StreamKindMetrics}, stream.kind, "schedule %s has invalid stream kind", sched.schedule)
		}
	}

	// Unknown (legacy) schedules have no stream; scheduleView omits the
	// fields rather than inventing names.
	unknown := streamForSchedule("made_up")
	require.Empty(t, unknown.name)
	require.Empty(t, unknown.kind)

	view := scheduleView(SyncSchedule{
		ID:       uuid.New(),
		Schedule: "made_up",
		Kind:     SyncKindTime,
	})
	require.Nil(t, view.Stream)
	require.Nil(t, view.StreamKind)
}

func setScheduleDisabled(t *testing.T, ctx context.Context, conn *pgxpool.Pool, store *Store, configID uuid.UUID, schedule string, disabled bool) SyncSchedule {
	t.Helper()

	var updated SyncSchedule
	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		updated, err = store.setSyncScheduleDisabledWithTx(ctx, tx, configID, schedule, disabled)
		return err
	}))
	return updated
}
