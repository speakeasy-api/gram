package assistants

import (
	"context"
	"fmt"
	"testing"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type ateRetirementBackend struct {
	*switchBackend
	core   *ServiceCore
	reaped bool
}

func (b *ateRetirementBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	return b.core.kitATELifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		_, err := assistantrepo.New(locked.LifecycleTx).GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: locked.ProjectID, ID: locked.ID})
		if err != nil {
			return fmt.Errorf("reload ATE retirement authority: %w", err)
		}
		b.reaped = true
		return nil
	})
}

func TestATERuntimeSwitchRetirementReacquiresPostcommitAuthority(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "ate_retirement_commit")
	require.NoError(t, err)
	project, assistant, _, thread := insertAssistantFixture(t, db)
	q := assistantrepo.New(db)
	_, err = q.CancelPendingAssistantThreadEvents(t.Context(), assistantrepo.CancelPendingAssistantThreadEventsParams{ProjectID: project, ThreadID: thread, PendingStatus: eventStatusPending, CancelledStatus: eventStatusCancelled})
	require.NoError(t, err)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{RuntimeVersion: 3, AssistantID: assistant, ProjectID: project}))
	runtimeID := insertActiveV2RuntimeRow(t, db, project, assistant, thread, runtimeBackendATE, runtimeStateActive, `{}`)
	core := &ServiceCore{db: db}
	backend := &ateRetirementBackend{switchBackend: &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}, core: core, reaped: false}
	core.runtime = backend
	tx := testenv.BeginTx(t, t.Context(), db)
	queries := assistantrepo.New(tx)
	require.NoError(t, queries.AcquireAssistantAdvisoryLock(t.Context(), assistant.String()))
	release, err := core.prepareRuntimeSelection(t.Context(), queries, tx, project, assistant, runtimeSelectionChange{Version: new(2), ExpectedVersion: new(3)})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	release()
	require.True(t, backend.reaped, "retirement must not reuse the committed selection transaction")
	row, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, runtimeStateStopped, row.State)
}
