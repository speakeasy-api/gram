package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestCorrelateClaudePromptsWorkflow_DrainsWhenActivityReportsMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	callCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CorrelateClaudePromptsArgs) (*activities.CorrelateClaudePromptsResult, error) {
			callCount++
			return &activities.CorrelateClaudePromptsResult{
				HasMore: callCount == 1,
			}, nil
		},
		activity.RegisterOptions{Name: "CorrelateClaudePrompts"},
	)

	env.ExecuteWorkflow(CorrelateClaudePromptsWorkflow, CorrelateClaudePromptsParams{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, callCount)
}
