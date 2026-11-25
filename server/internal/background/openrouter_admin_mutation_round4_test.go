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

func completeAdminMutation(t *testing.T, env *testsuite.TestWorkflowEnvironment, id string, delay time.Duration, completed *atomic.Int32, token func() int64, onComplete func()) {
	t.Helper()
	env.RegisterDelayedCallback(func() {
		operationToken := token()
		require.NotZero(t, operationToken)
		env.UpdateWorkflow(OpenRouterAdminCompleteUpdate, id, &testsuite.TestUpdateCallback{
			OnReject: func(err error) { require.NoError(t, err) },
			OnAccept: func() {},
			OnComplete: func(_ any, err error) {
				require.NoError(t, err)
				if completed != nil {
					completed.Add(1)
				}
				if onComplete != nil {
					onComplete()
				}
			},
		}, operationToken)
	}, delay)
}

func runCompleteUpdateWorkflow(t *testing.T, schedule func(*testsuite.TestWorkflowEnvironment, *atomic.Int64, *atomic.Int32, *atomic.Int32)) (int32, int64, int64) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var cursor atomic.Int64
	var patches atomic.Int32
	var completed atomic.Int32
	var reconciled atomic.Int64
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		return cursor.Load(), nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		current := cursor.Load()
		if current > checkpoint.Cursor {
			patches.Add(1)
		}
		reconciled.Store(current)
		return current, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	schedule(env, &cursor, &completed, &patches)
	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	return patches.Load(), int64(completed.Load()), reconciled.Load()
}

func TestOpenRouterAdminCompleteUpdateAcknowledgesWithoutFollowingIdleRun(t *testing.T) {
	t.Parallel()
	patches, completed, reconciled := runCompleteUpdateWorkflow(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64, completed *atomic.Int32, _ *atomic.Int32) {
		cursor.Store(1)
		completeAdminMutation(t, env, "a-complete", time.Millisecond, completed, func() int64 { return -1 }, nil)
	})
	require.Zero(t, patches, "a Complete without its Begin baseline cannot safely scan historical evidence")
	require.EqualValues(t, 1, completed)
	require.Zero(t, reconciled)
}

func TestOpenRouterAdminCompleteUpdateAcceptedDuringClose(t *testing.T) {
	t.Parallel()
	patches, completed, _ := runCompleteUpdateWorkflow(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64, completed *atomic.Int32, patches *atomic.Int32) {
		var token atomic.Int64
		armAdminMutationWithToken(t, env, "a-begin", time.Millisecond, &token, func() { cursor.Store(1) })
		completeAdminMutation(t, env, "a-close-race", testOpenRouterAdminGuardDelay-time.Millisecond, completed, token.Load, func() {
			require.EqualValues(t, 1, patches.Load(), "Complete must reconcile the token returned by Begin")
		})
	})
	require.EqualValues(t, 1, patches)
	require.EqualValues(t, 1, completed)
}

func TestOpenRouterAdminCompleteUpdateStartsSuccessorAfterClose(t *testing.T) {
	t.Parallel()
	patches, completed, _ := runCompleteUpdateWorkflow(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64, completed *atomic.Int32, _ *atomic.Int32) {
		// This fresh run models atomic Update-With-Start choosing the start arm
		// after the predecessor has already closed.
		cursor.Store(7)
		completeAdminMutation(t, env, "successor-complete", time.Millisecond, completed, func() int64 { return -1 }, nil)
	})
	require.Zero(t, patches, "the predecessor guard already reconciled before a successor can start")
	require.EqualValues(t, 1, completed)
}

func TestOpenRouterAdminConcurrentCompletesAreAcknowledgedAtLatestCursor(t *testing.T) {
	t.Parallel()
	patches, completed, reconciled := runCompleteUpdateWorkflow(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64, completed *atomic.Int32, _ *atomic.Int32) {
		for i := int64(1); i <= 3; i++ {
			value := i
			env.RegisterDelayedCallback(func() { cursor.Store(value) }, time.Duration(2*i-1)*time.Millisecond)
			completeAdminMutation(t, env, string(rune('a'+i-1)), time.Duration(2*i)*time.Millisecond, completed, func() int64 { return -1 }, nil)
		}
	})
	require.Zero(t, patches)
	require.EqualValues(t, 3, completed)
	require.Zero(t, reconciled)
}

func TestOpenRouterAdminTimedOutCompleteUpdateStillConverges(t *testing.T) {
	t.Parallel()
	patches, completed, reconciled := runCompleteUpdateWorkflow(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64, completed *atomic.Int32, _ *atomic.Int32) {
		cursor.Store(9)
		completeAdminMutation(t, env, "accepted-before-client-timeout", time.Millisecond, completed, func() int64 { return -1 }, nil)
	})
	require.Zero(t, patches)
	require.EqualValues(t, 1, completed, "accepted update must finish after its caller stops waiting")
	require.Zero(t, reconciled)
}
