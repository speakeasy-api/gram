package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/signins"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	signInRevocationSweepScheduleID = "v1:sign-in-revocation-sweep-schedule"
	signInRevocationSweepWorkflowID = signInRevocationSweepScheduleID + "/scheduled"
	signInRevocationSweepInterval   = 5 * time.Minute

	// Allow lateness up to one interval minus 1s; skip older missed ticks.
	signInRevocationSweepCatchupWindow = signInRevocationSweepInterval - time.Second

	// A full batch means more may wait.
	signInRevocationSweepBatchSize = signins.SweepBatchSize
	// Batches run one at a time without activity retries (the next run is the
	// retry), so the run fits its timeout: 2 × 2m activity ≤ 4m30s < interval.
	signInRevocationSweepMaxBatchesPerRun = 2
	signInRevocationSweepRunTimeout       = 4*time.Minute + 30*time.Second
)

// SignInRevocationSweepWorkflow retries sign-in revocation deliveries
// (revoked JTIs, WorkOS sessions and dashboard epochs) that failed right after their
// revocation committed. It is a safety net: the request that revoked already
// tried once, and issuance and refresh already refuse revoked sign-ins.
//
// Temporal actions/month ≈ 8,640 starts + ~8,640 activities ≈ 17,300 per
// namespace (up to one more activity per run while a backlog drains).
// Fixed: one fleet-wide schedule, not per tenant.
func SignInRevocationSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    1,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities
	for range signInRevocationSweepMaxBatchesPerRun {
		var claimed int
		if err := workflow.ExecuteActivity(ctx, a.SweepSignInRevocations).Get(ctx, &claimed); err != nil {
			return fmt.Errorf("sweep sign-in revocations: %w", err)
		}
		if claimed < signInRevocationSweepBatchSize {
			return nil
		}
	}
	return nil
}

func AddSignInRevocationSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		CatchupWindow: signInRevocationSweepCatchupWindow,
		ID:            signInRevocationSweepScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: signInRevocationSweepInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 signInRevocationSweepWorkflowID,
			Workflow:           SignInRevocationSweepWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: signInRevocationSweepRunTimeout,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("create sign-in revocation sweep schedule: %w", err)
	}
	return nil
}
