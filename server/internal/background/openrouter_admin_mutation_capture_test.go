package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
)

func runCaptureFailureWorkflow(t *testing.T, failures int32) (int32, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetTestTimeout(2 * time.Second)
	var attempts atomic.Int32
	var updateErr error
	env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) {
		if attempts.Add(1) <= failures {
			return 0, errors.New("capture unavailable")
		}
		return 7, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
		return checkpoint.Cursor, nil
	}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(OpenRouterAdminBeginUpdate, "begin", &testsuite.TestUpdateCallback{
			OnReject:   func(err error) { require.NoError(t, err) },
			OnAccept:   func() {},
			OnComplete: func(_ any, err error) { updateErr = err },
		})
	}, time.Millisecond)
	env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
	require.NoError(t, env.GetWorkflowError())
	return attempts.Load(), updateErr
}

func TestOpenRouterAdminPersistentCaptureFailureCompletesAndCloses(t *testing.T) {
	t.Parallel()
	attempts, err := runCaptureFailureWorkflow(t, 100)
	require.ErrorContains(t, err, "capture unavailable")
	require.EqualValues(t, 3, attempts)
}

func TestOpenRouterAdminTransientCaptureFailureRetriesAndLaterFreshRunWorks(t *testing.T) {
	t.Parallel()
	attempts, err := runCaptureFailureWorkflow(t, 2)
	require.NoError(t, err)
	require.EqualValues(t, 3, attempts)

	attempts, err = runCaptureFailureWorkflow(t, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, attempts)
}
