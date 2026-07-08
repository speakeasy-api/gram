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
	refreshBillingUsageBatchSize                   = 5
	billingUsagePauseEveryBatches                  = 2
	refreshBillingUsageActivityStartToCloseTimeout = 60 * time.Second
	refreshBillingUsageActivityMaximumAttempts     = 3
	// Reserve more than the worst-case retry path for one batch:
	// 3 activity attempts plus 10s/15s retry backoffs and Temporal jitter.
	refreshBillingUsageActivityWorstCaseRetryWindow = 5 * time.Minute
	refreshBillingUsageWorkflowRunTimeout           = 30 * time.Minute
	refreshBillingUsagesWaitInterval                = 10 * time.Second
)

type RefreshBillingUsageInput struct {
	OrgIDs           []string
	StartIndex       int
	FailedBatchCount int
	FailedOrgCount   int
}

type RefreshBillingUsageClient struct {
	TemporalEnv *tenv.Environment
}

func (c *RefreshBillingUsageClient) StartRefreshBillingUsage(ctx context.Context) (client.WorkflowRun, error) {
	id := "v1:refresh-billing-usage"
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                 id,
		TaskQueue:          string(c.TemporalEnv.Queue()),
		WorkflowRunTimeout: refreshBillingUsageWorkflowRunTimeout,
		// Allow restarting if needed
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           nil,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})
}

func RefreshBillingUsageWorkflow(ctx workflow.Context, input RefreshBillingUsageInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting billing usage refreshing")

	// Configure activity options with retry policy
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: refreshBillingUsageActivityStartToCloseTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    refreshBillingUsageActivityMaximumAttempts,
			InitialInterval:    refreshBillingUsagesWaitInterval,
			BackoffCoefficient: 1.5,
			// Temporal automatically adds some jitter to retries here
		},
	})

	var a *Activities
	orgIDs := input.OrgIDs
	// Initial call to workflow - no orgs yet, need to fetch them
	if len(orgIDs) == 0 {
		err := workflow.ExecuteActivity(ctx, a.GetAllOrganizations).Get(ctx, &orgIDs)
		if err != nil {
			logger.Error("Failed to get all organizations", "error", err)
			return fmt.Errorf("failed to get all organizations: %w", err)
		}

		input = RefreshBillingUsageInput{
			OrgIDs:           orgIDs,
			StartIndex:       0,
			FailedBatchCount: 0,
			FailedOrgCount:   0,
		}
	}

	logger.Info("Retrieved organizations for billing usage refreshing", "count", len(orgIDs))

	startIndex := min(max(input.StartIndex, 0), len(orgIDs))

	failedBatchCount := input.FailedBatchCount
	failedOrgCount := input.FailedOrgCount
	batchesProcessed := 0
	continueAsNew := func(nextStartIndex int) error {
		nextInput := RefreshBillingUsageInput{
			OrgIDs:           orgIDs,
			StartIndex:       nextStartIndex,
			FailedBatchCount: failedBatchCount,
			FailedOrgCount:   failedOrgCount,
		}
		logger.Info(
			"Continuing billing usage refresh as new",
			"next_start_index", nextStartIndex,
			"total_count", len(orgIDs),
			"failed_batch_count", failedBatchCount,
			"failed_org_count", failedOrgCount,
		)
		return workflow.NewContinueAsNewError(ctx, RefreshBillingUsageWorkflow, nextInput)
	}

	for i := startIndex; i < len(orgIDs); i += refreshBillingUsageBatchSize {
		end := min(i+refreshBillingUsageBatchSize, len(orgIDs))
		batch := orgIDs[i:end]

		if err := workflow.ExecuteActivity(ctx, a.RefreshBillingUsage, batch).Get(ctx, nil); err != nil {
			logger.Error("Failed to refresh billing usage batch", "error", err, "batch_start", i)
			failedBatchCount++
			failedOrgCount += len(batch)
		}

		// Persist durable TUM billing-cycle snapshots for the same batch. Runs
		// as its own activity so Polar refresh failures and snapshot failures
		// don't mask each other.
		if err := workflow.ExecuteActivity(ctx, a.SnapshotBillingCycleUsage, batch).Get(ctx, nil); err != nil {
			logger.Error("Failed to snapshot billing cycle usage batch", "error", err, "batch_start", i)
			failedBatchCount++
			failedOrgCount += len(batch)
		}

		batchesProcessed++
		if end < len(orgIDs) {
			// Polar's usage endpoints are rate-limited, so keep a deterministic
			// pause after small groups of batches instead of after every batch.
			if batchesProcessed%billingUsagePauseEveryBatches == 0 {
				if err := workflow.Sleep(ctx, refreshBillingUsagesWaitInterval); err != nil {
					logger.Error("Failed to sleep to pause between billing usage batches", "error", err)
					return fmt.Errorf("sleep between billing usage batches: %w", err)
				}
			}

			if shouldContinueRefreshBillingUsageAsNew(ctx) {
				return continueAsNew(end)
			}
		}
	}

	if failedBatchCount > 0 {
		logger.Warn(
			"Billing usage refreshing completed with failed batches",
			"failed_batch_count", failedBatchCount,
			"failed_org_count", failedOrgCount,
			"total_count", len(orgIDs),
		)
		return nil
	}

	logger.Info("Billing usage refreshing completed successfully", "total_count", len(orgIDs))
	return nil
}

func shouldContinueRefreshBillingUsageAsNew(ctx workflow.Context) bool {
	info := workflow.GetInfo(ctx)
	if info.GetContinueAsNewSuggested() {
		return true
	}

	runTimeout := info.WorkflowRunTimeout
	if runTimeout == 0 {
		runTimeout = refreshBillingUsageWorkflowRunTimeout
	}
	if runTimeout == 0 || info.WorkflowStartTime.IsZero() {
		return false
	}

	elapsed := workflow.Now(ctx).Sub(info.WorkflowStartTime)
	return elapsed+refreshBillingUsageActivityWorstCaseRetryWindow >= runTimeout
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
			Args:               []any{RefreshBillingUsageInput{OrgIDs: nil, StartIndex: 0, FailedBatchCount: 0, FailedOrgCount: 0}},
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: refreshBillingUsageWorkflowRunTimeout,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to refresh billing usage schedule: %w", err)
	}

	return nil
}
