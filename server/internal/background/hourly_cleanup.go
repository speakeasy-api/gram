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

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// hourlyCleanup is the policy shared by the global retention sweeps: one
// schedule per task queue that fires every hour, starts one workflow, and runs
// one bounded activity with a small retry budget.
type hourlyCleanup struct {
	// name identifies the sweep in its schedule ID and in errors.
	name string

	// activityTimeout bounds one activity attempt.
	activityTimeout time.Duration

	// maxAttempts is the activity's total attempt budget per run.
	maxAttempts int

	// retryInterval is the backoff before the first retry.
	retryInterval time.Duration

	// backoffCoefficient multiplies the backoff before each later retry.
	backoffCoefficient float64

	// schedulingMargin is headroom for queueing and workflow tasks on top of
	// the attempts and backoff.
	schedulingMargin time.Duration
}

// hourlyCleanupInterval is how often every hourly cleanup schedule fires.
const hourlyCleanupInterval = time.Hour

func (c hourlyCleanup) activityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: c.activityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    int32(c.maxAttempts), //nolint:gosec // a small constant attempt budget.
			InitialInterval:    c.retryInterval,
			BackoffCoefficient: c.backoffCoefficient,
		},
	}
}

// runTimeout covers every attempt, the backoff between them, and queue and
// workflow-task headroom, so a run never times out mid-retry.
func (c hourlyCleanup) runTimeout() time.Duration {
	budget := time.Duration(c.maxAttempts) * c.activityTimeout
	interval := c.retryInterval
	for attempt := 1; attempt < c.maxAttempts; attempt++ {
		budget += interval
		interval = time.Duration(float64(interval) * c.backoffCoefficient)
	}
	return budget + c.schedulingMargin
}

// scheduleID names the task queue, so a preview owns its own copy of the
// schedule and never re-points the shared one.
func (c hourlyCleanup) scheduleID(temporalEnv *tenv.Environment) string {
	return fmt.Sprintf("v1:%s:%s", c.name, temporalEnv.Queue())
}

// ensureSchedule creates the hourly schedule for wf, or reconciles an existing
// one's run timeout and catch-up window while preserving its operator pause
// state.
func (c hourlyCleanup) ensureSchedule(ctx context.Context, temporalEnv *tenv.Environment, wf any) error {
	id := c.scheduleID(temporalEnv)
	sc := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
		ID:            id,
		CatchupWindow: hourlyCleanupInterval - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: hourlyCleanupInterval}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           wf,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: c.runTimeout(),
		},
	}
	_, err := sc.Create(ctx, options)
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		err = sc.GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				schedule := input.Description.Schedule
				action, ok := schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || action.TaskQueue != string(temporalEnv.Queue()) {
					return nil, fmt.Errorf("%s schedule is owned by another task queue", c.name)
				}
				if action.WorkflowRunTimeout == c.runTimeout() &&
					schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				action.WorkflowRunTimeout = c.runTimeout()
				if schedule.Policy == nil {
					var policy client.SchedulePolicies
					schedule.Policy = &policy
				}
				schedule.Policy.CatchupWindow = options.CatchupWindow
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
		if err != nil {
			return fmt.Errorf("update %s schedule: %w", c.name, err)
		}
	case err != nil:
		return fmt.Errorf("create %s schedule: %w", c.name, err)
	}
	return nil
}
