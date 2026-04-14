package background

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

func TriggerCronWorkflow(ctx workflow.Context, input bgtriggers.ScheduleWorkflowInput) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	var result activities.ProcessScheduledTriggerResult
	if err := workflow.ExecuteActivity(ctx, a.ProcessScheduledTrigger, activities.ProcessScheduledTriggerInput{
		TriggerInstanceID: input.TriggerInstanceID,
		FiredAt:           workflow.Now(ctx).UTC().Format(time.RFC3339Nano),
	}).Get(ctx, &result); err != nil {
		return err
	}

	if result.Task == nil {
		return nil
	}

	return workflow.ExecuteActivity(ctx, a.DispatchTrigger, activities.DispatchTriggerInput(result)).Get(ctx, nil)
}

func TriggerDispatchWorkflow(ctx workflow.Context, input bgtriggers.TriggerDispatchWorkflowInput) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	return workflow.ExecuteActivity(ctx, a.DispatchTrigger, activities.DispatchTriggerInput{
		Task: &input.Task,
	}).Get(ctx, nil)
}
