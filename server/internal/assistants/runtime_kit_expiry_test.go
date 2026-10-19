package assistants

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

func TestKitLegacyExpiryNeverStopsOnIdleUnknownOrError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		idle *uint64
		err  error
	}{
		{name: "fully idle", idle: new(uint64(3600))},
		{name: "unknown settlement"},
		{name: "status unavailable", err: errors.New("status unavailable")},
		{name: "busy", idle: new(uint64(0))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stops atomic.Int64
			core := &ServiceCore{runtime: testRuntimeBackend{backend: runtimeBackendLocal, statusResult: RuntimeBackendStatus{Configured: true, IdleSeconds: tc.idle}, statusErr: tc.err, stopCalls: &stops}}
			result, err := core.expireRuntime(t.Context(), assistantRuntimeRecord{RuntimeVersion: 3}, 0)
			require.NoError(t, err)
			require.False(t, result.Stopped)
			require.Positive(t, result.RemainingSeconds)
			require.Zero(t, stops.Load())
		})
	}
}

func TestKitLegacyExpiryPreservesPendingAdmission(t *testing.T) {
	t.Parallel()
	conn, err := assistantsInfra.CloneTestDatabase(t, "kit_expiry_pending")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)
	_, err = conn.Exec(t.Context(), "UPDATE assistants SET runtime_version = 3 WHERE id = $1 AND project_id = $2", assistantID, projectID)
	require.NoError(t, err)
	runtimeID := insertActiveV2RuntimeRow(t, conn, projectID, assistantID, threadID, runtimeBackendLocal, runtimeStateActive, `{}`)
	// Make the row eligible for the old idle sweep while a pending admission
	// has not reached the guest. Both expiry entry points must leave it alone.
	_, err = conn.Exec(t.Context(), "UPDATE assistant_runtimes SET warm_until = $3 WHERE id = $1 AND project_id = $2", runtimeID, projectID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	var stops atomic.Int64
	core := newProvisioningCore(t, conn)
	core.runtime = testRuntimeBackend{backend: runtimeBackendLocal, statusResult: RuntimeBackendStatus{Configured: true}, stopCalls: &stops}
	result, err := core.ExpireThreadRuntime(t.Context(), projectID, threadID, 0)
	require.NoError(t, err)
	require.True(t, result.Stopped, "finish the legacy expiry loop, not the Kit runtime")
	affected := make(map[uuid.UUID]struct{})
	require.Zero(t, core.sweepIdleRuntimes(t.Context(), time.Now(), affected))
	require.Empty(t, affected)
	require.Zero(t, stops.Load())
	row, err := assistantrepo.New(conn).GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	require.NoError(t, err)
	require.EqualValues(t, 3, row.RuntimeVersion)
	require.Equal(t, runtimeStateActive, row.State)
	require.False(t, row.Ended)
	require.False(t, row.Deleted)
	runtime := testRuntimeRecord(runtimeBackendLocal)
	runtime.ID, runtime.ProjectID, runtime.AssistantID, runtime.RuntimeVersion = runtimeID, projectID, assistantID, 3
	event, claimed, err := core.claimRuntimeEvent(t.Context(), runtime, threadID)
	require.NoError(t, err)
	require.True(t, claimed, "legacy expiry must not fence the queued Kit admission")
	require.Equal(t, eventStatusProcessing, event.Status)
}
