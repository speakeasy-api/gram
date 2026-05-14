package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestProcessWorkOSUserEventsWorkflowID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:process-workos-user-events", processWorkOSUserEventsWorkflowID)
	require.Equal(t, "v1:process-workos-user-events:user_123", processWorkOSUserEventsWorkflowIDForParams(ProcessWorkOSUserEventsWorkflowParams{WorkOSUserID: "user_123"}))
	require.Equal(t, "v1:process-workos-user-events/signal", processWorkOSUserEventsDebounceSignal(ProcessWorkOSUserEventsWorkflowParams{WorkOSUserID: "user_123"}))
}

func TestProcessWorkOSUserEventsWorkflow_CompletesWhenNoMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ProcessWorkOSUserEventsParams) (*activities.ProcessWorkOSUserEventsResult, error) {
			require.Equal(t, "user_workflow", params.WorkOSUserID)
			return &activities.ProcessWorkOSUserEventsResult{
				SinceEventID: "",
				LastEventID:  "event_user",
				HasMore:      false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSUserEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSUserEventsWorkflow, ProcessWorkOSUserEventsWorkflowParams{WorkOSUserID: "user_workflow"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result activities.ProcessWorkOSUserEventsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.HasMore)
}

func TestProcessWorkOSUserEventsWorkflowDebounced_ContinuesAsNewOnHasMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSUserEventsParams) (*activities.ProcessWorkOSUserEventsResult, error) {
			return &activities.ProcessWorkOSUserEventsResult{HasMore: true}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSUserEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSUserEventsWorkflowDebounced, ProcessWorkOSUserEventsWorkflowParams{WorkOSUserID: "user_workflow"})

	require.True(t, env.IsWorkflowCompleted())
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "ProcessWorkOSUserEventsWorkflowDebounced", canErr.WorkflowType.Name)
}
