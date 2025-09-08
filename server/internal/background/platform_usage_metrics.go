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

// safely wait for polar rate limits
const (
	platformUsageMetricsBatchSize     = 20
	platformUsageMetricsRetryInterval = 5 * time.Second
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
	var a *Activities
	var allMetrics []activities.PlatformUsageMetrics
	err := workflow.ExecuteActivity(ctx, a.CollectPlatformUsageMetrics).Get(ctx, &allMetrics)
	if err != nil {
		logger.Error("Failed to collect platform usage metrics", "error", err)
		return fmt.Errorf("failed to collect platform usage metrics: %w", err)
	}

	// Process metrics in batches
	for i := 0; i < len(allMetrics); i += platformUsageMetricsBatchSize {
		end := min(i+platformUsageMetricsBatchSize, len(allMetrics))

		batch := allMetrics[i:end]

		err := workflow.ExecuteActivity(ctx, a.FirePlatformUsageMetrics, batch).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to fire platform usage metrics batch", "error", err, "batch_start", i)
			return fmt.Errorf("failed to fire platform usage metrics batch starting at %d: %w", i, err)
		}
	}

	for i := 0; i < len(allMetrics); i += platformUsageMetricsBatchSize {
		end := min(i+platformUsageMetricsBatchSize, len(allMetrics))

		batch := allMetrics[i:end]

		orgIDs := make([]string, len(batch))
		for j, metric := range batch {
			orgIDs[j] = metric.OrganizationID
		}

		err := workflow.ExecuteActivity(ctx, a.FreeTierReportingUsageMetrics, orgIDs).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to compile free tier reporting usage metrics batch", "error", err, "batch_start", i)
			return fmt.Errorf("failed to to compile free tier reporting usage metrics batct %d: %w", i, err)
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
