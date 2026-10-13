package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	ssoSignInRevocationSweepScheduleID = "v1:sso-sign-in-revocation-sweep-schedule"
	ssoSignInRevocationSweepWorkflowID = ssoSignInRevocationSweepScheduleID + "/scheduled"
	ssoSignInRevocationSweepInterval   = 5 * time.Minute

	// Allow lateness up to one interval minus 1s; skip older missed ticks.
	ssoSignInRevocationSweepCatchupWindow = ssoSignInRevocationSweepInterval - time.Second

	// A full batch means more may wait.
	ssoSignInRevocationSweepBatchSize        = ssosignins.SweepBatchSize
	// Batches run one at a time without activity retries (the next run is the
	// retry), so the run fits its timeout: 2 × 2m activity ≤ 4m30s < interval.
	ssoSignInRevocationSweepMaxBatchesPerRun = 2
	ssoSignInRevocationSweepRunTimeout       = 4*time.Minute + 30*time.Second
)

// SSOSignInRevocationSweepWorkflow retries SSO sign-in revocation deliveries
// (revoked JTIs, WorkOS sessions and dashboard epochs) that failed right after their
// revocation committed. It is a safety net: the request that revoked already
// tried once, and issuance and refresh already refuse revoked sign-ins.
//
// Temporal actions/month ≈ 8,640 starts + ~8,640 activities ≈ 17,300 per
// namespace (up to one more activity per run while a backlog drains).
// Fixed: one fleet-wide schedule, not per tenant.
func SSOSignInRevocationSweepWorkflow(ctx workflow.Context) error {
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
	for range ssoSignInRevocationSweepMaxBatchesPerRun {
		var claimed int
		if err := workflow.ExecuteActivity(ctx, a.SweepSSOSignInRevocations).Get(ctx, &claimed); err != nil {
			return fmt.Errorf("sweep sso sign-in revocations: %w", err)
		}
		if claimed < ssoSignInRevocationSweepBatchSize {
			return nil
		}
	}
	return nil
}

func AddSSOSignInRevocationSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := createScheduleWithCatchup(ctx, temporalEnv.Client().ScheduleClient(), client.ScheduleOptions{
		CatchupWindow: ssoSignInRevocationSweepCatchupWindow,
		ID:            ssoSignInRevocationSweepScheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: ssoSignInRevocationSweepInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 ssoSignInRevocationSweepWorkflowID,
			Workflow:           SSOSignInRevocationSweepWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: ssoSignInRevocationSweepRunTimeout,
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("create sso sign-in revocation sweep schedule: %w", err)
	}
	return nil
}
