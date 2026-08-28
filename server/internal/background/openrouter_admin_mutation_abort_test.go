package background

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
)

func runAbortWorkflow(t *testing.T, abortOnAccept bool) (int32, int32) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var reconciles atomic.Int32
	var completed atomic.Int32
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		return 4, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		reconciles.Add(1)
		return checkpoint.Cursor, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(OpenRouterAdminBeginUpdate, "begin", &testsuite.TestUpdateCallback{
			OnReject: func(err error) { require.NoError(t, err) },
			OnAccept: func() {
				if abortOnAccept {
					env.SignalWorkflow(OpenRouterAdminAbortSignal, nil)
				}
			},
			OnComplete: func(_ any, err error) {
				require.NoError(t, err)
				completed.Add(1)
				if !abortOnAccept {
					env.SignalWorkflow(OpenRouterAdminAbortSignal, nil)
				}
			},
		})
	}, time.Millisecond)
	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	return reconciles.Load(), completed.Load()
}

func TestOpenRouterAdminAbortIsTerminalWithoutGuardReconcile(t *testing.T) {
	t.Parallel()
	reconciles, completed := runAbortWorkflow(t, false)
	require.Zero(t, reconciles)
	require.EqualValues(t, 1, completed)
}

func TestOpenRouterAdminAbortCloseRaceFinishesAcceptedHandlerAndFreshRunWorks(t *testing.T) {
	t.Parallel()
	reconciles, completed := runAbortWorkflow(t, true)
	require.Zero(t, reconciles)
	require.EqualValues(t, 1, completed)

	attempts, err := runCaptureFailureWorkflow(t, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, attempts)
}
