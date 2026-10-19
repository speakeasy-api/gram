package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	directoryReviewSweepActivityTimeout = 10 * time.Minute
	directoryReviewSweepMaxAttempts     = 3
	directoryReviewSweepRunTimeout      = 45 * time.Minute
)

// DirectoryReviewSweepWorkflow reconciles directory review decisions for every
// organization with directory sync. It is a safety net behind the directory
// event handlers and mapping writes, which raise and settle items as they
// happen; the sweep catches what they cannot see, such as a directory deleted
// as a whole, and records each organization's baseline on its first run.
//
// Temporal actions/month: about 1,440 per namespace (720 hourly starts plus
// one activity each), at most about 2,880 with retries. Fixed: the activity
// loops over organizations itself rather than starting one per organization.
func DirectoryReviewSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: directoryReviewSweepActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    directoryReviewSweepMaxAttempts,
			InitialInterval:    time.Minute,
			BackoffCoefficient: 2,
		},
	})
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.SweepDirectoryReviews).Get(ctx, nil)
}

// SweepDirectoryReviews reconciles every organization's directory review
// items. Each organization commits on its own, so a retry only repeats work
// that is already idempotent.
func (a *Activities) SweepDirectoryReviews(ctx context.Context) error {
	if err := directoryreview.Sweep(ctx, a.db); err != nil {
		return fmt.Errorf("sweep directory reviews: %w", err)
	}
	return nil
}

// AddDirectoryReviewSweepSchedule registers the hourly sweep. It belongs to
// the shared task queue only: preview databases are clones of dev's, and the
// sweep's results drive admin notifications.
func AddDirectoryReviewSweepSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	id := fmt.Sprintf("v1:directory-review-sweep:%s", temporalEnv.Queue())
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:            id,
		CatchupWindow: time.Hour - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           DirectoryReviewSweepWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: directoryReviewSweepRunTimeout,
		},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create directory review sweep schedule: %w", err)
	}
	return nil
}
