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

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	directoryReviewEmailsInterval        = 15 * time.Minute
	directoryReviewEmailsActivityTimeout = 5 * time.Minute
	directoryReviewEmailsMaxAttempts     = 3
	directoryReviewEmailsRunTimeout      = 14 * time.Minute
)

// DirectoryReviewEmailsWorkflow emails organization admins about directory
// changes waiting for a decision: an announcement once new items have been
// quiet for activities.DirectoryReviewEmailQuietPeriod, then weekly reminders
// while any stay pending, up to activities.DirectoryReviewMaxReminders. It
// runs on its own schedule, shorter than the sweep's, so the quiet period
// bounds how long an announcement waits.
//
// Temporal actions/month: about 5,760 per namespace (2,880 starts every 15
// minutes plus one activity each), at most about 11,520 with retries. Fixed:
// the activity loops over organizations itself.
func DirectoryReviewEmailsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: directoryReviewEmailsActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    directoryReviewEmailsMaxAttempts,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
		},
	})
	var a *Activities
	args := activities.SendDirectoryReviewEmailsArgs{Now: workflow.Now(ctx)}
	return workflow.ExecuteActivity(ctx, a.SendDirectoryReviewEmails, args).Get(ctx, nil)
}

// SendDirectoryReviewEmails sends the announcements and reminders that are due.
func (a *Activities) SendDirectoryReviewEmails(ctx context.Context, args activities.SendDirectoryReviewEmailsArgs) error {
	if err := a.directoryReviewEmails.Send(ctx, args); err != nil {
		return fmt.Errorf("send directory review emails: %w", err)
	}
	return nil
}

// AddDirectoryReviewEmailsSchedule registers the email step on the shared task
// queue only, like the sweep: preview databases are clones of dev's, and
// their admins must not be emailed.
func AddDirectoryReviewEmailsSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	id := fmt.Sprintf("v1:directory-review-emails:%s", temporalEnv.Queue())
	sc := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
		ID:            id,
		CatchupWindow: directoryReviewEmailsInterval - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: directoryReviewEmailsInterval}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           DirectoryReviewEmailsWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: directoryReviewEmailsRunTimeout,
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
					return nil, fmt.Errorf("directory review emails schedule is owned by another task queue")
				}
				if action.WorkflowRunTimeout == directoryReviewEmailsRunTimeout &&
					schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				action.WorkflowRunTimeout = directoryReviewEmailsRunTimeout
				if schedule.Policy == nil {
					var policy client.SchedulePolicies
					schedule.Policy = &policy
				}
				schedule.Policy.CatchupWindow = options.CatchupWindow
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
		if err != nil {
			return fmt.Errorf("update directory review emails schedule: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("create directory review emails schedule: %w", err)
	}
	return nil
}
