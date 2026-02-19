package background

import (
	"context"
	"fmt"
	"time"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// safely wait for polar rate limits
const (
	refreshBillingUsageBatchSize     = 25
	refreshBillingUsagesWaitInterval = 10 * time.Second
)

type RefreshBillingUsageClient struct {
	TemporalEnv *tenv.Environment
}

func (c *RefreshBillingUsageClient) StartRefreshBillingUsage(ctx context.Context) (client.WorkflowRun, error) {
	id := "v1:refresh-billing-usage"
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id,
		TaskQueue: string(c.TemporalEnv.Queue()),
		// Allow restarting if needed
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, RefreshBillingUsageWorkflow)
}

func RefreshBillingUsageWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting billing usage refreshing")

	// Configure activity options with retry policy
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    refreshBillingUsagesWaitInterval,
			BackoffCoefficient: 1.5,
			// Temporal automatically adds some jitter to retries here
		},
	})

	// Get all organizations
	var a *Activities
	var orgIDs []string
	err := workflow.ExecuteActivity(ctx, a.GetAllOrganizations).Get(ctx, &orgIDs)
	if err != nil {
		logger.Error("Failed to get all organizations", "error", err)
		return fmt.Errorf("failed to get all organizations: %w", err)
	}

	logger.Info("Retrieved organizations for billing usage refreshing", "count", len(orgIDs))

	// Process organizations in batches
	for i := 0; i < len(orgIDs); i += refreshBillingUsageBatchSize {
		end := min(i+refreshBillingUsageBatchSize, len(orgIDs))
		batch := orgIDs[i:end]

		err := workflow.ExecuteActivity(ctx, a.RefreshBillingUsage, batch).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to refresh billing usage batch", "error", err, "batch_start", i)
			return fmt.Errorf("failed to refresh billing usage batch starting at %d: %w", i, err)
		}

		if err = workflow.Sleep(ctx, refreshBillingUsagesWaitInterval); err != nil {
			logger.Error("Failed to sleep to pause between batches", "error", err)
		}
	}

	logger.Info("Billing usage refreshing completed successfully")
	return nil
}

func AddRefreshBillingUsageSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleID := "v1:refresh-billing-usage-schedule"
	workflowID := "v1:refresh-billing-usage-schedule/scheduled"

	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: scheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{
					Every: 1 * time.Hour, // This should run minimum hourly to maintain fresh period usage cache
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 workflowID,
			Workflow:           RefreshBillingUsageWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: 15 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to refresh billing usage schedule: %w", err)
	}

	return nil
}
