package triggers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWakeCreationRechecksLifecycleAfterSelectionLock(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	ctx := t.Context()
	selection := testenv.BeginTx(t, ctx, f.db)
	require.NoError(t, assistantrepo.New(selection).AcquireAssistantAdvisoryLock(ctx, f.assistantID.String()))

	result := make(chan error, 1)
	go func() {
		_, err := f.app.CreateWakeInstance(ctx, triggers.CreateWakeInstanceParams{
			OrganizationID: "org-trigger-test", ProjectID: f.projectID, AssistantID: f.assistantID,
			Name: "Wake", TargetDisplay: "Assistant", FireAt: time.Now().Add(time.Hour), CorrelationID: "thread",
		})
		result <- err
	}()

	testenv.WaitForQueryBlockedBy(t, ctx, f.db, testenv.BackendPID(selection), "%AcquireAssistantAdvisoryLock :exec%")
	_, err := selection.Exec(ctx, "UPDATE assistants SET status = 'inactive' WHERE id = $1 AND project_id = $2", f.assistantID, f.projectID)
	require.NoError(t, err)
	require.NoError(t, selection.Commit(ctx))
	require.ErrorContains(t, <-result, "wake assistant is not active")
	var wakes int
	require.NoError(t, f.db.QueryRow(ctx, "SELECT count(*) FROM trigger_instances WHERE project_id = $1 AND target_ref = $2 AND definition_slug = 'wake'", f.projectID, f.assistantID.String()).Scan(&wakes))
	require.Zero(t, wakes)
}
