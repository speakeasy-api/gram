package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

const (
	platformUsageMetricsBatchSize     = 25
	platformUsageMetricsRetryInterval = 1 * time.Second
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
			MaximumAttempts:    3,
			InitialInterval:    platformUsageMetricsRetryInterval,
			BackoffCoefficient: 1.5,
			// Temporal automatically adds some jitter to retries here
		},
	})

	// Collect all platform usage metrics
	var collectActivity *activities.CollectPlatformUsageMetrics
	var allMetrics []activities.PlatformUsageMetrics
	err := workflow.ExecuteActivity(ctx, collectActivity.Do).Get(ctx, &allMetrics)
	if err != nil {
		logger.Error("Failed to collect platform usage metrics", "error", err)
		return fmt.Errorf("failed to collect platform usage metrics: %w", err)
	}

	// Process metrics in batches
	var fireActivity *activities.FirePlatformUsageMetrics

	for i := 0; i < len(allMetrics); i += platformUsageMetricsBatchSize {
		end := min(i+platformUsageMetricsBatchSize, len(allMetrics))

		batch := allMetrics[i:end]

		err := workflow.ExecuteActivity(ctx, fireActivity.Do, batch).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to fire platform usage metrics batch", "error", err, "batch_start", i)
			return fmt.Errorf("failed to fire platform usage metrics batch starting at %d: %w", i, err)
		}
	}

	logger.Info("Platform usage metrics collection and firing completed successfully")
	return nil
}

func AddPlatformUsageMetricsSchedule(ctx context.Context, temporalClient client.Client) error {
	scheduleID := "v1:collect-platform-usage-metrics-schedule"
	workflowID := "v1:collect-platform-usage-metrics/scheduled"

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
			Workflow:           CollectPlatformUsageMetricsWorkflow,
			TaskQueue:          string(TaskQueueMain),
			WorkflowRunTimeout: 10 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create platform usage metrics schedule: %w", err)
	}

	return nil
}
