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
	sc := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
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
	}
	_, err := sc.Create(ctx, options)
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		// Bring an existing schedule's policy up to date without touching its
		// state, so an operator's pause and note survive a deploy.
		err = sc.GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				schedule := input.Description.Schedule
				action, ok := schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || action.TaskQueue != string(temporalEnv.Queue()) {
					return nil, fmt.Errorf("directory review sweep schedule is owned by another task queue")
				}
				if action.WorkflowRunTimeout == directoryReviewSweepRunTimeout &&
					schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				action.WorkflowRunTimeout = directoryReviewSweepRunTimeout
				if schedule.Policy == nil {
					var policy client.SchedulePolicies
					schedule.Policy = &policy
				}
				schedule.Policy.CatchupWindow = options.CatchupWindow
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
		if err != nil {
			return fmt.Errorf("update directory review sweep schedule: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("create directory review sweep schedule: %w", err)
	}
	return nil
}
