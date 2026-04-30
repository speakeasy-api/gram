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

func TestProcessWorkOSOrganizationEventsWorkflowID(t *testing.T) {
	t.Parallel()

	id := processWorkOSOrganizationEventsWorkflowID(ProcessWorkOSEventsParams{WorkOSOrganizationID: "org_01HZ"})
	require.Equal(t, "v1:process-workos-org-events:org_01HZ", id)

	sig := processWorkOSOrganizationEventsDebounceSignal(ProcessWorkOSEventsParams{WorkOSOrganizationID: "org_01HZ"})
	require.Equal(t, "v1:process-workos-org-events:org_01HZ/signal", sig)
}

func TestProcessWorkOSOrganizationEventsWorkflow_CompletesWhenNoMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSOrganizationEventsParams) (*activities.ProcessWorkOSOrganizationEventsResult, error) {
			return &activities.ProcessWorkOSOrganizationEventsResult{
				SinceEventID: "",
				LastEventID:  "event_01HZA",
				HasMore:      false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSOrganizationEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSOrganizationEventsWorkflow, ProcessWorkOSEventsParams{WorkOSOrganizationID: "org_01HZ"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result ProcessWorkOSEventsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.HasMore)
}

func TestProcessWorkOSOrganizationEventsWorkflow_SignalsHasMoreToWrapper(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSOrganizationEventsParams) (*activities.ProcessWorkOSOrganizationEventsResult, error) {
			return &activities.ProcessWorkOSOrganizationEventsResult{
				SinceEventID: "",
				LastEventID:  "event_01HZA",
				HasMore:      true,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSOrganizationEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSOrganizationEventsWorkflow, ProcessWorkOSEventsParams{WorkOSOrganizationID: "org_01HZ"})

	// Inner workflow MUST NOT issue ContinueAsNew when HasMore is true — that
	// path is owned by the Debounce wrapper. The inner workflow returns
	// (result, nil) and lets reenqueue trigger the next run.
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result ProcessWorkOSEventsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.HasMore)
}

func TestProcessWorkOSOrganizationEventsWorkflowDebounced_ContinuesAsNewOnHasMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSOrganizationEventsParams) (*activities.ProcessWorkOSOrganizationEventsResult, error) {
			return &activities.ProcessWorkOSOrganizationEventsResult{
				SinceEventID: "",
				LastEventID:  "event_01HZA",
				HasMore:      true,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSOrganizationEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSOrganizationEventsWorkflowDebounced, ProcessWorkOSEventsParams{WorkOSOrganizationID: "org_01HZ"})

	require.True(t, env.IsWorkflowCompleted())

	// The wrapper's ContinueAsNew MUST target the debounced wrapper itself, not
	// the inner workflow. If it targeted the inner, subsequent runs would lose
	// debounce semantics (no signal coalescing on the next page).
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "ProcessWorkOSOrganizationEventsWorkflowDebounced", canErr.WorkflowType.Name)
}

func TestProcessWorkOSOrganizationEventsWorkflowDebounced_CompletesWithoutSignals(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSOrganizationEventsParams) (*activities.ProcessWorkOSOrganizationEventsResult, error) {
			return &activities.ProcessWorkOSOrganizationEventsResult{HasMore: false}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSOrganizationEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSOrganizationEventsWorkflowDebounced, ProcessWorkOSEventsParams{WorkOSOrganizationID: "org_01HZ"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
