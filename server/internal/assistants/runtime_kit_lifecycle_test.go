package assistants

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type kitLifecycleBackend struct {
	testRuntimeBackend
	stop func(context.Context, assistantRuntimeRecord) error
}

func (b kitLifecycleBackend) CheckpointIdleRuntime(ctx context.Context, r assistantRuntimeRecord) error {
	return b.stop(ctx, r)
}

func TestKitLifecycleCapabilityIsProductionOnly(t *testing.T) {
	t.Parallel()
	r := testRuntimeRecord(runtimeBackendATE)
	r.RuntimeVersion = 3
	_, err := kitIdleCheckpointCapability(testRuntimeBackend{backend: runtimeBackendATE}, r)
	require.Error(t, err, "ordinary Stop must not opt into checkpoint expiry")
	b := kitLifecycleBackend{testRuntimeBackend: testRuntimeBackend{backend: runtimeBackendATE}}
	_, err = kitIdleCheckpointCapability(&telemetryRuntimeBackend{inner: &versionedRuntimeBackend{kit: b}}, r)
	require.NoError(t, err)
	r.Backend = runtimeBackendLocal
	_, err = kitIdleCheckpointCapability(b, r)
	require.Error(t, err)
	r.Backend, r.RuntimeVersion = runtimeBackendATE, 2
	_, err = kitIdleCheckpointCapability(b, r)
	require.Error(t, err)
}

func TestKitLifecycleCheckpointAdmissionFence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		backend     string
		eventStatus string
		warm        bool
		grace       bool
		stopError   error
		wantStop    bool
	}{
		{name: "settled checkpoint preserves authority", backend: runtimeBackendATE, eventStatus: eventStatusCompleted, wantStop: true},
		{name: "pending admission", backend: runtimeBackendATE, eventStatus: eventStatusPending},
		{name: "processing admission", backend: runtimeBackendATE, eventStatus: eventStatusProcessing},
		{name: "idle grace", backend: runtimeBackendATE, eventStatus: eventStatusCompleted, grace: true},
		{name: "warm process", backend: runtimeBackendATE, eventStatus: eventStatusCompleted, warm: true},
		{name: "local unchanged", backend: runtimeBackendLocal, eventStatus: eventStatusCompleted},
		{name: "unknown settlement fails closed", backend: runtimeBackendATE, eventStatus: eventStatusCompleted, stopError: errors.New("settlement unavailable"), wantStop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, err := assistantsInfra.CloneTestDatabase(t, "kit_lifecycle")
			require.NoError(t, err)
			project, assistant, _, thread := insertAssistantFixture(t, conn)
			q := assistantrepo.New(conn)
			require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{RuntimeVersion: 3, AssistantID: assistant, ProjectID: project}))
			runtimeID := insertActiveV2RuntimeRow(t, conn, project, assistant, thread, tc.backend, runtimeStateActive, `{"test":true}`)
			core := newProvisioningCore(t, conn)
			r := testRuntimeRecord(tc.backend)
			r.ID, r.ProjectID, r.AssistantID, r.RuntimeVersion = runtimeID, project, assistant, 3
			event, claimed, err := core.claimRuntimeEvent(t.Context(), r, thread)
			require.NoError(t, err)
			require.True(t, claimed)
			require.NoError(t, q.SetAssistantThreadEventStatus(t.Context(), assistantrepo.SetAssistantThreadEventStatusParams{Status: tc.eventStatus, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, ID: event.ID, ProjectID: project}))
			now := time.Now()
			warmUntil := now.Add(-time.Hour)
			if tc.grace {
				warmUntil = now.Add(-runtimeIdleSweepGrace / 2)
			}
			if tc.warm {
				warmUntil = now.Add(time.Hour)
			}
			require.NoError(t, q.SetAssistantRuntimeActive(t.Context(), assistantrepo.SetAssistantRuntimeActiveParams{ActiveState: runtimeStateActive, WarmUntil: pgtype.Timestamptz{Time: warmUntil, Valid: true}, RuntimeID: runtimeID, ProjectID: project}))
			called := false
			core.runtime = kitLifecycleBackend{testRuntimeBackend: testRuntimeBackend{backend: tc.backend}, stop: func(ctx context.Context, got assistantRuntimeRecord) error {
				called = true
				require.Equal(t, runtimeID, got.ID)
				// A competing admission cannot enter while Stop fences/checkpoints.
				contender := testenv.BeginTx(t, ctx, conn)
				defer func() { _ = contender.Rollback(context.Background()) }()
				lockCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
				require.Error(t, assistantrepo.New(contender).AcquireAssistantAdvisoryLock(lockCtx, assistant.String()))
				return tc.stopError
			}}
			stopped, err := core.checkpointIdleKitRuntime(t.Context(), project, assistant, runtimeID, now)
			if tc.stopError != nil {
				require.ErrorIs(t, err, tc.stopError)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantStop, called)
			require.Equal(t, tc.wantStop && tc.stopError == nil, stopped)
			row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: project})
			require.NoError(t, err)
			require.Equal(t, runtimeStateActive, row.State)
			require.False(t, row.Ended)
			require.False(t, row.Deleted)
			require.Equal(t, !stopped, row.WarmUntil.Valid)
			require.JSONEq(t, `{"test":true}`, string(row.BackendMetadataJson))
		})
	}
}

func TestKitLifecycleNewAssistantSelectionOnly(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "kit_new_selection")
	require.NoError(t, err)
	project, existing, _, _ := insertAssistantFixture(t, conn)
	core := newProvisioningCore(t, conn)
	for _, version := range []int16{0, 3, 4} {
		core.newAssistantRuntimeVersion = version
		created, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", fmt.Sprintf("New assistant %d", version), "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, false, nil, nil)
		require.NoError(t, err)
		expected := int16(2)
		if version == 3 {
			expected = 3
		}
		require.Equal(t, expected, created.RuntimeVersion)
		row, err := assistantrepo.New(conn).GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: created.ID})
		require.NoError(t, err)
		require.Equal(t, expected, row.RuntimeVersion)
	}
	row, err := assistantrepo.New(conn).GetAssistant(t.Context(), assistantrepo.GetAssistantParams{ProjectID: project, AssistantID: existing})
	require.NoError(t, err)
	require.EqualValues(t, 2, row.RuntimeVersion)
}

func TestUnservableKitRuntimeBackendRetiresAndReplacesCleanly(t *testing.T) {
	t.Parallel()

	t.Run("router refuses execution and settles switch", func(t *testing.T) {
		t.Parallel()
		v2Backend := &stubRuntimeBackend{name: runtimeBackendGKE}
		v3Backend := &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendATE}}
		router := &versionedRuntimeBackend{legacy: v2Backend, kit: v3Backend}

		record := testRuntimeRecord(runtimeBackendGKE)
		record.RuntimeVersion = 3
		_, err := router.Ensure(t.Context(), record)
		require.ErrorIs(t, err, errKitRetiredBackend)
		require.ErrorIs(t, router.RunTurn(t.Context(), record, runTurnRequest{}), errKitRetiredBackend)
		require.ErrorIs(t, router.Reap(t.Context(), record), errKitRetiredBackend)
		require.Zero(t, v2Backend.runTurnCount)
		require.Zero(t, v3Backend.runTurnCount)

		ready, err := router.PrepareRuntimeSwitch(t.Context(), assistantRecord{}, []assistantRuntimeRecord{record}, 2)
		require.NoError(t, err)
		require.True(t, ready.Settled)
		require.True(t, ready.DestinationReady)
	})

	t.Run("turn dispatch retires and requests re-admission", func(t *testing.T) {
		t.Parallel()
		conn, err := assistantsInfra.CloneTestDatabase(t, "kit_unservable_turn")
		require.NoError(t, err)
		project, assistant, _, thread := insertAssistantFixture(t, conn)
		q := assistantrepo.New(conn)
		require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{RuntimeVersion: 3, AssistantID: assistant, ProjectID: project}))
		runtimeID := insertActiveV2RuntimeRow(t, conn, project, assistant, thread, runtimeBackendGKE, runtimeStateActive, `{"sandbox":"unservable-backend"}`)

		var logs bytes.Buffer
		core := newProvisioningCore(t, conn)
		core.logger = slog.New(slog.NewTextHandler(&logs, nil))
		core.runtime = &versionedRuntimeBackend{
			legacy: &stubRuntimeBackend{name: runtimeBackendGKE},
			kit:    &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendATE}},
		}

		res, err := core.ProcessThreadEvents(t.Context(), project, thread)
		require.NoError(t, err)
		require.True(t, res.RetryAdmission, "unservable runtime row must trigger re-admission on the active backend")
		require.False(t, res.RuntimeActive)
		require.Contains(t, logs.String(), "retiring leftover Kit runtime referencing removed backend")

		row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: project})
		require.NoError(t, err)
		require.Equal(t, runtimeStateFailed, row.State)
		require.True(t, row.Deleted)
	})

	t.Run("idle sweep and reapers terminally clear row", func(t *testing.T) {
		t.Parallel()
		conn, err := assistantsInfra.CloneTestDatabase(t, "kit_unservable_reaper")
		require.NoError(t, err)
		project, assistant, _, thread := insertAssistantFixture(t, conn)
		q := assistantrepo.New(conn)
		require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{RuntimeVersion: 3, AssistantID: assistant, ProjectID: project}))
		runtimeID := insertActiveV2RuntimeRow(t, conn, project, assistant, thread, runtimeBackendGKE, runtimeStateActive, `{"sandbox":"unservable-backend"}`)

		var logs bytes.Buffer
		core := newProvisioningCore(t, conn)
		core.logger = slog.New(slog.NewTextHandler(&logs, nil))
		core.runtime = &versionedRuntimeBackend{
			legacy: &stubRuntimeBackend{name: runtimeBackendGKE},
			kit:    &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendATE}},
		}

		stopped := core.sweepIdleKitRuntimes(t.Context(), time.Now())
		require.EqualValues(t, 1, stopped)
		require.Contains(t, logs.String(), "retiring leftover Kit runtime referencing removed backend")

		row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: project})
		require.NoError(t, err)
		require.Equal(t, runtimeStateFailed, row.State)
		require.True(t, row.Deleted)

		rec := assistantRuntimeRecord{
			ID:                  row.ID,
			AssistantThreadID:   row.AssistantThreadID,
			AssistantID:         row.AssistantID,
			ProjectID:           row.ProjectID,
			RuntimeVersion:      row.RuntimeVersion,
			Backend:             row.Backend,
			BackendMetadataJSON: row.BackendMetadataJson,
			State:               row.State,
		}
		require.True(t, core.reapStoppedRuntimeRow(t.Context(), rec))
		row, err = q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: project})
		require.NoError(t, err)
		require.Equal(t, runtimeStateReaped, row.State)

		require.True(t, core.reapRuntimeRow(t.Context(), rec))
	})
}
