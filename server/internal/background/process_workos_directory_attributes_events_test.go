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

func TestProcessWorkOSDirectoryAttributesEventsWorkflowID(t *testing.T) {
	t.Parallel()

	params := ProcessWorkOSDirectoryAttributesEventsWorkflowParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroup,
		EntityID:   "directory_group_123",
	}
	require.Equal(t, "v1:process-workos-directory-attributes-events", processWorkOSDirectoryAttributesEventsWorkflowID)
	require.Equal(t, "v1:process-workos-directory-attributes-events:group:directory_group_123", processWorkOSDirectoryAttributesEventsWorkflowIDForParams(params))
	require.Equal(t, "v1:process-workos-directory-attributes-events/signal", processWorkOSDirectoryAttributesEventsDebounceSignal(params))
}

func TestProcessWorkOSDirectoryAttributesEventsWorkflow_CompletesWhenNoMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, params activities.ProcessWorkOSDirectoryAttributesEventsParams) (*activities.ProcessWorkOSDirectoryAttributesEventsResult, error) {
			require.Equal(t, activities.WorkOSDirectoryAttributesEntityTypeGroup, params.EntityType)
			require.Equal(t, "directory_group_workflow", params.EntityID)
			return &activities.ProcessWorkOSDirectoryAttributesEventsResult{
				SinceEventID: "",
				LastEventID:  "event_group",
				HasMore:      false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSDirectoryAttributesEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSDirectoryAttributesEventsWorkflow, ProcessWorkOSDirectoryAttributesEventsWorkflowParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroup,
		EntityID:   "directory_group_workflow",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result activities.ProcessWorkOSDirectoryAttributesEventsResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.False(t, result.HasMore)
}

func TestProcessWorkOSDirectoryAttributesEventsWorkflowDebounced_ContinuesAsNewOnHasMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessWorkOSDirectoryAttributesEventsParams) (*activities.ProcessWorkOSDirectoryAttributesEventsResult, error) {
			return &activities.ProcessWorkOSDirectoryAttributesEventsResult{HasMore: true}, nil
		},
		activity.RegisterOptions{Name: "ProcessWorkOSDirectoryAttributesEvents"},
	)

	env.ExecuteWorkflow(ProcessWorkOSDirectoryAttributesEventsWorkflowDebounced, ProcessWorkOSDirectoryAttributesEventsWorkflowParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroupMembership,
		EntityID:   "directory_group_123:directory_user_123",
	})

	require.True(t, env.IsWorkflowCompleted())
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "ProcessWorkOSDirectoryAttributesEventsWorkflowDebounced", canErr.WorkflowType.Name)
}
