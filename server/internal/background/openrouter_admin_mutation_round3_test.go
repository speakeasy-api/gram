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

func runCursorGuardWorkflowTest(t *testing.T, operations func(*testsuite.TestWorkflowEnvironment, *atomic.Int64), wantPatches int32, wantBaseline int64) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var cursor atomic.Int64
	var patches atomic.Int32
	var reconciledBaseline atomic.Int64
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		return cursor.Load(), nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		reconciledBaseline.Store(checkpoint.Cursor)
		current := cursor.Load()
		if current > checkpoint.Cursor {
			patches.Add(1)
		}
		return current, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	operations(env, &cursor)
	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, wantPatches, patches.Load())
	require.Equal(t, wantBaseline, reconciledBaseline.Load())
}

func armAdminMutation(t *testing.T, env *testsuite.TestWorkflowEnvironment, id string, delay time.Duration, after func()) {
	t.Helper()
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(OpenRouterAdminBeginUpdate, id, &testsuite.TestUpdateCallback{
			OnReject: func(err error) { require.NoError(t, err) },
			OnAccept: func() {},
			OnComplete: func(_ any, err error) {
				require.NoError(t, err)
				if after != nil {
					after()
				}
			},
		})
	}, delay)
}

func TestOpenRouterAdminGuardRequiresDurableCommitProof(t *testing.T) {
	t.Parallel()
	t.Run("precommit crash and audit rollback do not PATCH", func(t *testing.T) {
		t.Parallel()
		runCursorGuardWorkflowTest(t, func(env *testsuite.TestWorkflowEnvironment, _ *atomic.Int64) {
			armAdminMutation(t, env, "rolled-back", time.Millisecond, nil)
		}, 0, 0)
	})
	t.Run("postcommit crash repairs", func(t *testing.T) {
		t.Parallel()
		runCursorGuardWorkflowTest(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64) {
			armAdminMutation(t, env, "committed", time.Millisecond, func() { cursor.Store(1) })
		}, 1, 0)
	})
}

func TestOpenRouterAdminBeginCapturesBaselineBeforeMutation(t *testing.T) {
	t.Parallel()
	runCursorGuardWorkflowTest(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64) {
		armAdminMutation(t, env, "baseline-first", time.Millisecond, func() {
			// This callback is the synchronous update completion: mutation can
			// begin only after the capture activity result is durable.
			cursor.Store(1)
		})
	}, 1, 0)
}

func TestOpenRouterAdminConcurrentBeginsPreserveOldestUnreconciledCursor(t *testing.T) {
	t.Parallel()
	runCursorGuardWorkflowTest(t, func(env *testsuite.TestWorkflowEnvironment, cursor *atomic.Int64) {
		armAdminMutation(t, env, "a", time.Millisecond, func() { cursor.Store(1) })
		armAdminMutation(t, env, "b", 2*time.Millisecond, func() { cursor.Store(2) })
		armAdminMutation(t, env, "c-rollback", 3*time.Millisecond, nil)
	}, 1, 0)
}
