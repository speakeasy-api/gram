package assistants

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

func TestKitIdleSweepProgressAcrossBatches(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "kit_sweep_progress")
	require.NoError(t, err)
	q := assistantrepo.New(conn)
	core := newProvisioningCore(t, conn)
	calls := map[uuid.UUID]int{}
	core.runtime = &versionedRuntimeBackend{kit: kitLifecycleBackend{testRuntimeBackend: testRuntimeBackend{backend: runtimeBackendGKE}, stop: func(_ context.Context, r assistantRuntimeRecord) error {
		calls[r.ID]++
		return nil
	}}}
	now := time.Now()
	var firstProject, firstRuntime, firstThread uuid.UUID
	for i := range runtimeIdleSweepBatch + 1 {
		project, assistant, thread := insertReapableProject(t, conn, fmt.Sprintf("kit-sweep-%d", i))
		require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{RuntimeVersion: 3, AssistantID: assistant, ProjectID: project}))
		if i == 0 {
			chatID := uuid.New()
			require.NoError(t, q.UpsertAssistantChat(t.Context(), assistantrepo.UpsertAssistantChatParams{
				ChatID: chatID, ProjectID: project, OrganizationID: "org-test",
			}))
			thread, err = q.UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{
				AssistantID: assistant, ProjectID: project, ChatID: chatID,
				CorrelationID: warmupCorrelationID, SourceKind: sourceKindWarmup, SourceRefJson: []byte("{}"),
			})
			require.NoError(t, err)
		}
		runtimeID := insertActiveV2RuntimeRow(t, conn, project, assistant, thread, runtimeBackendGKE, runtimeStateActive, `{"test":true}`)
		require.NoError(t, q.SetAssistantRuntimeActive(t.Context(), assistantrepo.SetAssistantRuntimeActiveParams{ActiveState: runtimeStateActive, RuntimeID: runtimeID, ProjectID: project, WarmUntil: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}}))
		if i == 0 {
			firstProject, firstRuntime, firstThread = project, runtimeID, thread
		}
	}
	require.EqualValues(t, runtimeIdleSweepBatch, core.sweepIdleKitRuntimes(t.Context(), now))
	require.EqualValues(t, 1, core.sweepIdleKitRuntimes(t.Context(), now))
	require.Zero(t, core.sweepIdleKitRuntimes(t.Context(), now))
	require.Len(t, calls, runtimeIdleSweepBatch+1)
	for _, count := range calls {
		require.Equal(t, 1, count)
	}
	row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: firstRuntime, ProjectID: firstProject})
	require.NoError(t, err)
	require.Equal(t, runtimeStateActive, row.State)
	require.False(t, row.Deleted)
	require.False(t, row.Ended)
	require.False(t, row.WarmUntil.Valid)
	require.JSONEq(t, `{"test":true}`, string(row.BackendMetadataJson))
	// Explicit warmup re-arms an already-active canonical row after Ensure.
	// There are no thread events: event completion cannot mask a missing re-arm.
	_, err = core.ProcessThreadEvents(t.Context(), firstProject, firstThread)
	require.NoError(t, err)
	resumed, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: firstRuntime, ProjectID: firstProject})
	require.NoError(t, err)
	require.True(t, resumed.WarmUntil.Valid)
	require.True(t, resumed.WarmUntil.Time.After(now))
	require.Zero(t, core.sweepIdleKitRuntimes(t.Context(), now))
	require.Zero(t, core.sweepIdleKitRuntimes(t.Context(), resumed.WarmUntil.Time.Add(time.Second)), "re-armed runtime retains idle-sweep grace")
	require.EqualValues(t, 1, core.sweepIdleKitRuntimes(t.Context(), resumed.WarmUntil.Time.Add(runtimeIdleSweepGrace+time.Second)))
	require.Equal(t, 2, calls[firstRuntime])
}
