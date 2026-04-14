package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

func TestTriggerCronWorkflowDispatchesScheduledTask(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	input := bgtriggers.ScheduleWorkflowInput{
		TriggerInstanceID: "11111111-1111-1111-1111-111111111111",
	}
	expectedTask := &bgtriggers.Task{
		TriggerInstanceID: input.TriggerInstanceID,
		DefinitionSlug:    "cron",
		TargetKind:        bgtriggers.TargetKindNoop,
		TargetRef:         "test-sink",
		TargetDisplay:     "Test Sink",
		EventID:           "event-123",
		CorrelationID:     input.TriggerInstanceID,
		RawPayload:        []byte(`{"ok":true}`),
	}

	var scheduledInput activities.ProcessScheduledTriggerInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, activityInput activities.ProcessScheduledTriggerInput) (*activities.ProcessScheduledTriggerResult, error) {
			scheduledInput = activityInput
			return &activities.ProcessScheduledTriggerResult{Task: expectedTask}, nil
		},
		activity.RegisterOptions{Name: "ProcessScheduledTrigger"},
	)

	var dispatchedInput activities.DispatchTriggerInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, activityInput activities.DispatchTriggerInput) error {
			dispatchedInput = activityInput
			return nil
		},
		activity.RegisterOptions{Name: "DispatchTrigger"},
	)

	env.ExecuteWorkflow(TriggerCronWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, input.TriggerInstanceID, scheduledInput.TriggerInstanceID)
	require.NotEmpty(t, scheduledInput.FiredAt)
	_, err := time.Parse(time.RFC3339Nano, scheduledInput.FiredAt)
	require.NoError(t, err)
	require.NotNil(t, dispatchedInput.Task)
	require.Equal(t, *expectedTask, *dispatchedInput.Task)
}

func TestTriggerCronWorkflowSkipsDispatchWhenNoTaskProduced(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessScheduledTriggerInput) (*activities.ProcessScheduledTriggerResult, error) {
			return &activities.ProcessScheduledTriggerResult{}, nil
		},
		activity.RegisterOptions{Name: "ProcessScheduledTrigger"},
	)

	dispatched := false
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.DispatchTriggerInput) error {
			dispatched = true
			return nil
		},
		activity.RegisterOptions{Name: "DispatchTrigger"},
	)

	env.ExecuteWorkflow(TriggerCronWorkflow, bgtriggers.ScheduleWorkflowInput{
		TriggerInstanceID: "11111111-1111-1111-1111-111111111111",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.False(t, dispatched)
}
