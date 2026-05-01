package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestAssistantThreadWorkflowBacksOffBeforeRetryAdmission(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(start)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessAssistantThreadInput) (*activities.ProcessAssistantThreadResult, error) {
			return &activities.ProcessAssistantThreadResult{
				AssistantID:       "11111111-1111-1111-1111-111111111111",
				WarmUntil:         "",
				RuntimeActive:     false,
				RetryAdmission:    true,
				ProcessedAnyEvent: false,
			}, nil
		},
		activity.RegisterOptions{Name: "ProcessAssistantThread"},
	)

	var signalTime time.Time
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SignalAssistantCoordinatorInput) error {
			signalTime = env.Now()
			return nil
		},
		activity.RegisterOptions{Name: "SignalAssistantCoordinator"},
	)

	env.ExecuteWorkflow(AssistantThreadWorkflow, AssistantThreadWorkflowInput{
		ThreadID:  "22222222-2222-2222-2222-222222222222",
		ProjectID: "33333333-3333-3333-3333-333333333333",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.GreaterOrEqual(t, signalTime.Sub(start), assistantRetryAdmissionBackoff)
}
