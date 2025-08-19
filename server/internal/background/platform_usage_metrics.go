package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type PlatformUsageMetricsClient struct {
	Temporal client.Client
}

func (c *PlatformUsageMetricsClient) StartCollectPlatformUsageMetrics(ctx context.Context) (client.WorkflowRun, error) {
	id := "v1:collect-platform-usage-metrics"
	return c.Temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: string(TaskQueueMain),
		// Allow restarting if needed
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, CollectPlatformUsageMetricsWorkflow)
}

func CollectPlatformUsageMetricsWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting platform usage metrics collection")

	// Configure activity options with retry policy
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	// Execute the activity to collect and record platform usage metrics
	var a *Activities
	err := workflow.ExecuteActivity(ctx, a.CollectPlatformUsageMetrics).Get(ctx, nil)
	if err != nil {
		logger.Error("failed to collect platform usage metrics", "error", err.Error())
		return fmt.Errorf("failed to collect platform usage metrics: %w", err)
	}

	logger.Info("Platform usage metrics collection completed successfully")
	return nil
}

func AddPlatformUsageMetricsSchedule(ctx context.Context, temporalClient client.Client) error {
	scheduleID := "v1:collect-platform-usage-metrics-schedule"
	workflowID := "v1:collect-platform-usage-metrics/scheduled"

	_, err := temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:   scheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{
					Every: 24 * time.Hour,
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        workflowID,	
			Workflow:  CollectPlatformUsageMetricsWorkflow,
			TaskQueue: string(TaskQueueMain),
		},
	})
	
	return err
}
