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
	reportBillingUsageBatchSize      = 25
	reportBillingUsagesRetryInterval = 1 * time.Second
)

type ReportBillingUsageClient struct {
	Temporal client.Client
}

func (c *ReportBillingUsageClient) StartReportBillingUsage(ctx context.Context) (client.WorkflowRun, error) {
	id := "v1:report-billing-usage"
	return c.Temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: string(TaskQueueMain),
		// Allow restarting if needed
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, ReportBillingUsageWorkflow)
}

func ReportBillingUsageWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting billing usage reporting")

	// Configure activity options with retry policy
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    reportBillingUsagesRetryInterval,
			BackoffCoefficient: 1.5,
			// Temporal automatically adds some jitter to retries here
		},
	})

	// Get all organizations
	var getAllOrgsActivity *activities.GetAllOrganizations
	var orgIDs []string
	err := workflow.ExecuteActivity(ctx, getAllOrgsActivity.Do).Get(ctx, &orgIDs)
	if err != nil {
		logger.Error("Failed to get all organizations", "error", err)
		return fmt.Errorf("failed to get all organizations: %w", err)
	}

	logger.Info("Retrieved organizations for billing usage reporting", "count", len(orgIDs))

	// Process organizations in batches
	var reportBillingActivity *activities.ReportBillingUsage

	for i := 0; i < len(orgIDs); i += reportBillingUsageBatchSize {
		end := min(i+reportBillingUsageBatchSize, len(orgIDs))
		batch := orgIDs[i:end]

		err := workflow.ExecuteActivity(ctx, reportBillingActivity.Do, batch).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to report billing usage batch", "error", err, "batch_start", i)
			return fmt.Errorf("failed to report billing usage batch starting at %d: %w", i, err)
		}
	}

	logger.Info("Billing usage reporting completed successfully")
	return nil
}

func AddReportBillingUsageSchedule(ctx context.Context, temporalClient client.Client) error {
	scheduleID := "v1:report-billing-usage-schedule"
	workflowID := "v1:report-billing-usage-schedule/scheduled"

	_, err := temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: scheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{
					Every: 1 * time.Hour,
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 workflowID,
			Workflow:           ReportBillingUsageWorkflow,
			TaskQueue:          string(TaskQueueMain),
			WorkflowRunTimeout: 10 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to report billing usage schedule: %w", err)
	}

	return nil
}
