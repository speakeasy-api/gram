package assistants

import (
	"context"
	"errors"
	"testing"
	"time"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

func TestKitGKEClosingFenceSurvivesTransactionLossBeforeClose(t *testing.T) {
	t.Parallel()
	core, db, key, _ := kitSessionFixture(t)
	_, err := db.Exec(t.Context(), "UPDATE assistant_runtimes SET backend = $1 WHERE id = $2", runtimeBackendGKE, key.RuntimeID)
	require.NoError(t, err)
	record := assistantRuntimeRecord{ID: key.RuntimeID, AssistantID: key.AssistantID, ProjectID: key.ProjectID}
	interrupted := errors.New("worker lost before close acknowledgement")
	err = core.kitGKELifecycle(t.Context(), record, func(ctx context.Context, r assistantRuntimeRecord) error {
		store := NewKitGKEPostgresAuthorityStore(db)
		next := []byte(`{"checkpoint":{"phase":"closing"}}`)
		if err := store.Save(ctx, r.LifecycleTx, r, r.BackendMetadataJSON, next); err != nil {
			return err
		}
		// Publication must already be visible before any external session close.
		row, err := assistantrepo.New(db).GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: r.ID, ProjectID: r.ProjectID})
		require.NoError(t, err)
		require.JSONEq(t, string(next), string(row.BackendMetadataJson))
		contender, err := db.Begin(ctx)
		require.NoError(t, err)
		lockCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		require.Error(t, assistantrepo.New(contender).AcquireAssistantAdvisoryLock(lockCtx, r.AssistantID.String()), "publication must not release the assistant fence")
		_ = contender.Rollback(ctx)
		// Losing the current transaction cannot roll back the published closing fence.
		require.NoError(t, r.LifecycleTx.Rollback(ctx))
		return interrupted
	})
	require.ErrorIs(t, err, interrupted)
	row, err := assistantrepo.New(db).GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{ID: key.RuntimeID, ProjectID: key.ProjectID})
	require.NoError(t, err)
	require.JSONEq(t, `{"checkpoint":{"phase":"closing"}}`, string(row.BackendMetadataJson))
	// Cleanup released the session fence and did not leak it through the pool.
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, assistantrepo.New(tx).AcquireAssistantAdvisoryLock(ctx, key.AssistantID.String()))
}
