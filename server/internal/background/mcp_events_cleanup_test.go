package background

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"testing"
)

func TestMCPEventsCleanupWorkflowRetriesTransientFailures(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("database unavailable")
		}
		return nil
	}, activity.RegisterOptions{Name: "CleanupMCPEvents"})
	env.ExecuteWorkflow(MCPEventsCleanupWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, calls)
}
