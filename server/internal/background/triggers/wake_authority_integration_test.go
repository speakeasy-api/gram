package triggers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
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
	err := assistantrepo.New(selection).SetAssistantStatus(ctx, assistantrepo.SetAssistantStatusParams{Status: "inactive", ID: f.assistantID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.NoError(t, selection.Commit(ctx))
	require.ErrorContains(t, <-result, "wake assistant is not active")
	wakes, err := triggerrepo.New(f.db).ListActiveTriggerInstancesByTarget(ctx, triggerrepo.ListActiveTriggerInstancesByTargetParams{ProjectID: f.projectID, TargetRef: f.assistantID.String(), TargetKind: triggers.TargetKindAssistant, DefinitionSlug: triggers.DefinitionSlugWake})
	require.NoError(t, err)
	require.Empty(t, wakes)
}
