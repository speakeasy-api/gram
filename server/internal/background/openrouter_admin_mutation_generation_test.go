package background

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
)

func TestOpenRouterAdminAbortRetiresOnlyItsOperation(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var cursor atomic.Int64
	var reconciles atomic.Int32
	var reconciledCursor atomic.Int64
	var tokenA int64
	var tokenB int64
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		return cursor.Load(), nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		reconciles.Add(1)
		reconciledCursor.Store(cursor.Load())
		return cursor.Load(), nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	updateBegin := func(id string, token *int64) {
		env.UpdateWorkflow(OpenRouterAdminBeginUpdate, id, &testsuite.TestUpdateCallback{
			OnReject: func(err error) { require.NoError(t, err) },
			OnAccept: func() {},
			OnComplete: func(result any, err error) {
				require.NoError(t, err)
				if value, ok := result.(int64); ok {
					*token = value
				}
			},
		})
	}
	env.RegisterDelayedCallback(func() { updateBegin("a", &tokenA) }, time.Millisecond)
	env.RegisterDelayedCallback(func() { updateBegin("b", &tokenB) }, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(OpenRouterAdminAbortSignal, tokenA) }, 3*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		cursor.Store(1)
		env.UpdateWorkflow(OpenRouterAdminCompleteUpdate, "complete-b", &testsuite.TestUpdateCallback{
			OnReject:   func(err error) { require.NoError(t, err) },
			OnAccept:   func() {},
			OnComplete: func(_ any, err error) { require.NoError(t, err) },
		}, tokenB)
	}, 4*time.Millisecond)

	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	require.NotZero(t, tokenA)
	require.NotZero(t, tokenB)
	require.NotEqual(t, tokenA, tokenB)
	require.EqualValues(t, 1, reconciles.Load())
	require.EqualValues(t, 1, reconciledCursor.Load())
}

func TestOpenRouterAdminConcurrentCompletionsDoNotBlockWorkflow(t *testing.T) {
	t.Parallel()
	const operationCount = 101
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetTestTimeout(2 * time.Second)
	var reconciles atomic.Int32
	var completed atomic.Int32
	tokens := make([]int64, operationCount)
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		return 0, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		reconciles.Add(1)
		return checkpoint.Cursor, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	env.RegisterDelayedCallback(func() {
		for i := range operationCount {
			index := i
			env.UpdateWorkflow(OpenRouterAdminBeginUpdate, fmt.Sprintf("begin-%d", i), &testsuite.TestUpdateCallback{
				OnReject: func(err error) { require.NoError(t, err) },
				OnAccept: func() {},
				OnComplete: func(result any, err error) {
					require.NoError(t, err)
					var ok bool
					tokens[index], ok = result.(int64)
					require.True(t, ok)
				},
			})
		}
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		for i, token := range tokens {
			require.NotZero(t, token)
			env.UpdateWorkflow(OpenRouterAdminCompleteUpdate, fmt.Sprintf("complete-%d", i), &testsuite.TestUpdateCallback{
				OnReject: func(err error) { require.NoError(t, err) },
				OnAccept: func() {},
				OnComplete: func(_ any, err error) {
					require.NoError(t, err)
					completed.Add(1)
				},
			}, token)
		}
	}, 2*time.Millisecond)

	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	require.EqualValues(t, operationCount, completed.Load())
	require.EqualValues(t, operationCount, reconciles.Load())
}

func TestOpenRouterAdminTokenMismatchAndCompleteRetryAreIdempotent(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var cursor atomic.Int64
	var reconciles atomic.Int32
	var completed atomic.Int32
	var tokenA int64
	var tokenB int64
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		return cursor.Load(), nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		reconciles.Add(1)
		return cursor.Load(), nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	updateBegin := func(id string, token *int64) {
		env.UpdateWorkflow(OpenRouterAdminBeginUpdate, id, &testsuite.TestUpdateCallback{
			OnReject: func(err error) { require.NoError(t, err) },
			OnAccept: func() {},
			OnComplete: func(result any, err error) {
				require.NoError(t, err)
				var ok bool
				*token, ok = result.(int64)
				require.True(t, ok)
			},
		})
	}
	var complete func(string)
	complete = func(id string) {
		env.UpdateWorkflow(OpenRouterAdminCompleteUpdate, id, &testsuite.TestUpdateCallback{
			OnReject: func(err error) { require.NoError(t, err) },
			OnAccept: func() {},
			OnComplete: func(_ any, err error) {
				require.NoError(t, err)
				if completed.Add(1) == 1 {
					complete("duplicate")
				} else {
					env.SignalWorkflow(OpenRouterAdminAbortSignal, tokenB)
				}
			},
		}, tokenA)
	}
	env.RegisterDelayedCallback(func() { updateBegin("a", &tokenA) }, time.Millisecond)
	env.RegisterDelayedCallback(func() { updateBegin("b", &tokenB) }, 2*time.Millisecond)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(OpenRouterAdminAbortSignal, tokenA+1000) }, 3*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		cursor.Store(1)
		complete("complete")
	}, 4*time.Millisecond)

	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	require.NotZero(t, tokenA)
	require.NotZero(t, tokenB)
	require.EqualValues(t, 2, completed.Load())
	require.EqualValues(t, 1, reconciles.Load())
}
