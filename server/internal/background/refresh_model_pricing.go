package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type RefreshModelPricingParams struct{}

// Called to start the workflow
func ExecuteRefreshModelPricingWorkflow(ctx context.Context, temporalClient client.Client) (client.WorkflowRun, error) {
	id := "v1:refresh-model-pricing"
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       id,
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowRunTimeout:       5 * time.Minute,
	}, RefreshModelPricingWorkflow, RefreshModelPricingParams{})
}

func RefreshModelPricingWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting model pricing refresh")

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	})

	var a *Activities
	err := workflow.ExecuteActivity(
		ctx,
		a.RefreshModelPricing,
		activities.RefreshModelPricingArgs{},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to refresh model pricing: %w", err)
	}

	logger.Info("Model pricing refresh completed successfully")
	return nil
}

func AddRefreshModelPricingSchedule(ctx context.Context, temporalClient client.Client) error {
	scheduleID := "v1:refresh-model-pricing-schedule"
	workflowID := "v1:refresh-model-pricing/scheduled"

	_, err := temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: scheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{
					Every: 24 * time.Hour,
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 workflowID,
			Workflow:           RefreshModelPricingWorkflow,
			TaskQueue:          string(TaskQueueMain),
			WorkflowRunTimeout: 5 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create refresh model pricing schedule: %w", err)
	}

	return nil
}
