package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestCorrelateClaudePromptsWorkflow_DrainsWhenActivityReportsMore(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	callCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.CorrelateClaudePromptsArgs) (*activities.CorrelateClaudePromptsResult, error) {
			callCount++
			if callCount == 2 {
				require.Equal(t, int64(25), input.AfterMessageSeq)
				require.Equal(t, int64(11), input.AfterEventSequence)
				require.Equal(t, int64(22), input.AfterEventTimeUnixNano)
			}
			return &activities.CorrelateClaudePromptsResult{
				HasMore:                callCount == 1,
				AfterMessageSeq:        25,
				AfterEventSequence:     11,
				AfterEventTimeUnixNano: 22,
			}, nil
		},
		activity.RegisterOptions{Name: "CorrelateClaudePrompts"},
	)

	env.ExecuteWorkflow(CorrelateClaudePromptsWorkflow, CorrelateClaudePromptsParams{
		ProjectID:              uuid.Nil,
		ChatID:                 uuid.Nil,
		SessionID:              "",
		AfterMessageSeq:        0,
		AfterEventSequence:     0,
		AfterEventTimeUnixNano: 0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, callCount)
}

func TestCorrelateClaudePromptsWorkflow_ContinuesAsNewNearRunTimeout(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkflowRunTimeout(correlateClaudePromptsActivityWorstCaseRetryWindow)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CorrelateClaudePromptsArgs) (*activities.CorrelateClaudePromptsResult, error) {
			return &activities.CorrelateClaudePromptsResult{
				HasMore:                true,
				AfterMessageSeq:        25,
				AfterEventSequence:     11,
				AfterEventTimeUnixNano: 22,
			}, nil
		},
		activity.RegisterOptions{Name: "CorrelateClaudePrompts"},
	)

	env.ExecuteWorkflow(CorrelateClaudePromptsWorkflow, CorrelateClaudePromptsParams{
		ProjectID:              uuid.Nil,
		ChatID:                 uuid.Nil,
		SessionID:              "session",
		AfterMessageSeq:        0,
		AfterEventSequence:     0,
		AfterEventTimeUnixNano: 0,
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, "CorrelateClaudePromptsWorkflow", continueAsNewErr.WorkflowType.Name)

	var nextInput CorrelateClaudePromptsParams
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))
	require.Equal(t, int64(25), nextInput.AfterMessageSeq)
	require.Equal(t, int64(11), nextInput.AfterEventSequence)
	require.Equal(t, int64(22), nextInput.AfterEventTimeUnixNano)
}
