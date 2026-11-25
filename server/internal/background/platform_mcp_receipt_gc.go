package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	platformMCPReceiptGCScheduleID = "v1:platform-mcp-receipt-gc-schedule"
	platformMCPReceiptGCWorkflowID = platformMCPReceiptGCScheduleID + "/scheduled"
	platformMCPReceiptGCInterval   = 1 * time.Hour

	// Allow lateness up to one interval minus 1s; skip older missed ticks.
	platformMCPReceiptGCCatchupWindow = platformMCPReceiptGCInterval - time.Second

	platformMCPReceiptGCBatchSize int32 = 2000

	// A run deletes at most this many batches and then continues as new, so a
	// large backlog drains within one tick in bounded runs instead of being
	// cut off by the run timeout.
	platformMCPReceiptGCMaxBatchesPerRun = 25

	// Covers a full run of batches with headroom for activity retries.
	platformMCPReceiptGCRunTimeout = 15 * time.Minute
)

// PlatformMCPReceiptGCWorkflow bounds the Platform MCP operation receipt
// table. A receipt is only reclaimed inline when the same user writes the same
// key on the same project again, and a project-creation receipt never is, so
// without this sweep expired receipts accumulate forever. Expiry is already
// enforced at read time, so a late or missed tick only delays reclamation.
//
// Temporal actions/month ≈ 720 starts + ~720 activities ≈ 1,440 per namespace
// (more only while a backlog drains: one activity per batch and one
// continue-as-new per platformMCPReceiptGCMaxBatchesPerRun batches). Fixed:
// one fleet-wide schedule, not per tenant.
func PlatformMCPReceiptGCWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities

	for range platformMCPReceiptGCMaxBatchesPerRun {
		var rows int64
		if err := workflow.ExecuteActivity(ctx, a.GCExpiredPlatformMCPReceipts, platformMCPReceiptGCBatchSize).Get(ctx, &rows); err != nil {
			return fmt.Errorf("gc expired platform mcp receipts: %w", err)
		}

		workflow.GetLogger(ctx).Info("platform mcp receipt gc batch completed", "rows_deleted", rows)

		if rows < int64(platformMCPReceiptGCBatchSize) {
			return nil
		}
	}
	return workflow.NewContinueAsNewError(ctx, PlatformMCPReceiptGCWorkflow)
}

// GCExpiredPlatformMCPReceipts deletes one batch of expired receipts and
// returns only the count, so no tenant data enters workflow history.
func (a *Activities) GCExpiredPlatformMCPReceipts(ctx context.Context, batchSize int32) (int64, error) {
	n, err := platformrepo.New(a.db).DeleteExpiredPlatformMCPOperationReceiptsBatch(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete expired platform mcp receipts: %w", err)
	}
	return n, nil
}

func AddPlatformMCPReceiptGCSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		CatchupWindow: platformMCPReceiptGCCatchupWindow,
		ID:            platformMCPReceiptGCScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: platformMCPReceiptGCInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 platformMCPReceiptGCWorkflowID,
			Workflow:           PlatformMCPReceiptGCWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: platformMCPReceiptGCRunTimeout,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("create platform mcp receipt gc schedule: %w", err)
	}
	return nil
}
