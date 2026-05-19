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

func TestProcessWorkOSMembershipEventsWorkflowID(t *testing.T) {
	t.Parallel()

	params := ProcessWorkOSMembershipEventsWorkflowParams{}
	require.Equal(t, "v1:process-workos-membership-events/signal", processWorkOSMembershipEventsDebounceSignal(params))
}

func TestProcessWorkOSMembershipEventsWorkflow_CompletesWhenNoMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSMembershipEventsParams) (*activities.ProcessWorkOSMembershipEventsResult, error) {
			return &activities.ProcessWorkOSMembershipEventsResult{
				SinceEventID: "",
				LastEventID:  "event_01HZA",
				HasMore:      false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSMembershipEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSMembershipEventsWorkflow, ProcessWorkOSMembershipEventsWorkflowParams{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result activities.ProcessWorkOSMembershipEventsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.HasMore)
}

func TestProcessWorkOSMembershipEventsWorkflowDebounced_ContinuesAsNewOnHasMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSMembershipEventsParams) (*activities.ProcessWorkOSMembershipEventsResult, error) {
			return &activities.ProcessWorkOSMembershipEventsResult{HasMore: true}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSMembershipEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSMembershipEventsWorkflowDebounced, ProcessWorkOSMembershipEventsWorkflowParams{})

	require.True(t, env.IsWorkflowCompleted())

	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "ProcessWorkOSMembershipEventsWorkflowDebounced", canErr.WorkflowType.Name)
}

func TestProcessWorkOSMembershipEventsWorkflowDebounced_CompletesWithoutSignals(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSMembershipEventsParams) (*activities.ProcessWorkOSMembershipEventsResult, error) {
			return &activities.ProcessWorkOSMembershipEventsResult{HasMore: false}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSMembershipEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSMembershipEventsWorkflowDebounced, ProcessWorkOSMembershipEventsWorkflowParams{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
