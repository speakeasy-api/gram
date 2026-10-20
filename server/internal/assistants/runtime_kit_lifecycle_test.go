package assistants

import (
	"context"
	"errors"
	"fmt"
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
	r := testRuntimeRecord(runtimeBackendGKE)
	r.RuntimeVersion = 3
	_, err := kitIdleCheckpointCapability(testRuntimeBackend{backend: runtimeBackendGKE}, r)
	require.Error(t, err, "ordinary Stop must not opt into checkpoint expiry")
	b := kitLifecycleBackend{testRuntimeBackend: testRuntimeBackend{backend: runtimeBackendGKE}}
	_, err = kitIdleCheckpointCapability(&telemetryRuntimeBackend{inner: &versionedRuntimeBackend{kit: b}}, r)
	require.NoError(t, err)
	r.Backend = runtimeBackendLocal
	_, err = kitIdleCheckpointCapability(b, r)
	require.Error(t, err)
	r.Backend, r.RuntimeVersion = runtimeBackendGKE, 2
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
		{name: "settled checkpoint preserves authority", backend: runtimeBackendGKE, eventStatus: eventStatusCompleted, wantStop: true},
		{name: "pending admission", backend: runtimeBackendGKE, eventStatus: eventStatusPending},
		{name: "processing admission", backend: runtimeBackendGKE, eventStatus: eventStatusProcessing},
		{name: "idle grace", backend: runtimeBackendGKE, eventStatus: eventStatusCompleted, grace: true},
		{name: "warm process", backend: runtimeBackendGKE, eventStatus: eventStatusCompleted, warm: true},
		{name: "local unchanged", backend: runtimeBackendLocal, eventStatus: eventStatusCompleted},
		{name: "unknown settlement fails closed", backend: runtimeBackendGKE, eventStatus: eventStatusCompleted, stopError: errors.New("settlement unavailable"), wantStop: true},
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
		created, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", fmt.Sprintf("New assistant %d", version), "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, false, nil)
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
