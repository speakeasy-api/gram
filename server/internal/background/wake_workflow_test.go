package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

func TestTriggerWakeWorkflowDispatchesAndMarksFired(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	input := bgtriggers.ScheduleWorkflowInput{
		TriggerInstanceID: "22222222-2222-2222-2222-222222222222",
	}
	expectedTask := &bgtriggers.Task{
		TriggerInstanceID: input.TriggerInstanceID,
		DefinitionSlug:    "wake",
		TargetKind:        bgtriggers.TargetKindAssistant,
		TargetRef:         "assistant-1",
		TargetDisplay:     "thread",
		EventID:           "event-w-1",
		CorrelationID:     "thread-corr",
		RawPayload:        []byte(`{"fired_at":"now"}`),
	}

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ProcessScheduledTriggerInput) (*activities.ProcessScheduledTriggerResult, error) {
			return &activities.ProcessScheduledTriggerResult{Task: expectedTask}, nil
		},
		activity.RegisterOptions{Name: "ProcessScheduledTrigger"},
	)

	var dispatched activities.DispatchTriggerInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.DispatchTriggerInput) error {
			dispatched = in
			return nil
		},
		activity.RegisterOptions{Name: "DispatchTrigger"},
	)

	var marked activities.MarkTriggerFiredInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.MarkTriggerFiredInput) error {
			marked = in
			return nil
		},
		activity.RegisterOptions{Name: "MarkTriggerFired"},
	)

	env.ExecuteWorkflow(TriggerWakeWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.NotNil(t, dispatched.Task)
	require.Equal(t, "thread-corr", dispatched.Task.CorrelationID)
	require.Equal(t, input.TriggerInstanceID, marked.TriggerInstanceID)
}

func TestTriggerWakeWorkflowMarksFiredEvenWhenNoTask(t *testing.T) {
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

	var marked activities.MarkTriggerFiredInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.MarkTriggerFiredInput) error {
			marked = in
			return nil
		},
		activity.RegisterOptions{Name: "MarkTriggerFired"},
	)

	input := bgtriggers.ScheduleWorkflowInput{
		TriggerInstanceID: "33333333-3333-3333-3333-333333333333",
	}
	env.ExecuteWorkflow(TriggerWakeWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.False(t, dispatched)
	require.Equal(t, input.TriggerInstanceID, marked.TriggerInstanceID)
}
