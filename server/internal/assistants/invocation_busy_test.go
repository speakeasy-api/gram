package assistants

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

func TestInvocationBusyPreservesQueuedEventBudget(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, classifyTurnError(&runtimeResponseError{StatusCode: http.StatusTooManyRequests, Body: ErrRuntimeInvocationBusy.Error()}), ErrRuntimeInvocationBusy)
	require.ErrorIs(t, classifyTurnError(&runtimeResponseError{StatusCode: http.StatusTooManyRequests, Body: "different-error"}), ErrRuntimeUnhealthy)
	db, err := assistantsInfra.CloneTestDatabase(t, "invocation_busy")
	require.NoError(t, err)
	project, assistant, _, _ := insertAssistantFixture(t, db)
	thread := seedThreadWithEvent(t, db, assistant, "busy-thread", "busy-thread", eventStatusPending)
	original, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: thread, ProjectID: project})
	require.NoError(t, err)
	core := newProvisioningCore(t, db)
	for range maxEventAttempts + 2 {
		event, ok, err := core.claimNextPendingEvent(t.Context(), project, thread)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 1, event.Attempts)
		require.NoError(t, core.resetEventToPending(t.Context(), project, event.ID, ErrRuntimeInvocationBusy))
	}
	row, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(t.Context(), assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{AssistantThreadID: thread, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, eventStatusPending, row.Status)
	require.Zero(t, row.Attempts)
	require.Equal(t, original.ID, row.ID)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, core.resetEventToPending(cancelled, project, row.ID, ErrRuntimeInvocationBusy))
}
