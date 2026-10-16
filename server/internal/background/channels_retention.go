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

	"github.com/speakeasy-api/gram/server/internal/channels"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	channelsRetentionInterval           = time.Hour
	channelsRetentionActivityTimeout    = 10 * time.Minute
	channelsRetentionMaxAttempts        = 3
	channelsRetentionRetryInterval      = time.Minute
	channelsRetentionBackoffCoefficient = 2
	channelsRetentionSchedulingMargin   = 7 * time.Minute
	// channelsRetentionMaxPassesPerAttempt bounds how much backlog one attempt
	// may drain. Each pass visits at most channels.DefaultRetention()
	// organizations and removes at most its batch per organization.
	channelsRetentionMaxPassesPerAttempt = 20
)

// ChannelsRetentionWorkflow retires Channels history and stale instances past
// their organization's retention window.
//
// One global hourly sweep, one activity per tick, no per-organization
// schedules and nothing in the request path. The activity's first query
// returns only the organizations that actually have something to retire, so a
// deployment where nobody uses Channels costs one indexed query per tick.
//
// Temporal actions/month per namespace: 720 starts + 720 activities = 1,440
// normally, 2,880 if every tick exhausts all three attempts. Fixed, not scaled
// by organizations, projects or messages. Each preview task queue registers
// its own copy of this schedule and adds the same fixed 1,440.
func ChannelsRetentionWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: channelsRetentionActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    channelsRetentionMaxAttempts,
			InitialInterval:    channelsRetentionRetryInterval,
			BackoffCoefficient: channelsRetentionBackoffCoefficient,
		},
	})
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.CleanupChannelsHistory).Get(ctx, nil)
}

// CleanupChannelsHistory drains the retention backlog in bounded passes.
//
// Every pass is independently committed and idempotent: the selecting queries
// only return rows that are still eligible, so a retry neither double-counts
// nor re-removes. Each attempt starts with a fresh pass budget; exhausting it
// is retryable rather than fatal, and the next hourly tick picks up whatever
// is left.
func (a *Activities) CleanupChannelsHistory(ctx context.Context) error {
	retention := channels.DefaultRetention()
	return drainChannelsRetention(ctx, func(ctx context.Context) (channels.RetentionResult, error) {
		return channels.RunRetention(ctx, a.db, a.logger, retention, time.Now())
	})
}

// drainChannelsRetention repeats a bounded pass until the backlog is clear or
// the attempt's budget runs out.
func drainChannelsRetention(ctx context.Context, sweep func(context.Context) (channels.RetentionResult, error)) error {
	for range channelsRetentionMaxPassesPerAttempt {
		result, err := sweep(ctx)
		if err != nil {
			return fmt.Errorf("sweep channels retention: %w", err)
		}
		if !result.Truncated {
			return nil
		}
	}
	return errors.New("channels retention per-attempt pass budget exhausted")
}

// channelsRetentionRunTimeout covers every attempt, the backoff between them,
// and queue and workflow-task headroom.
func channelsRetentionRunTimeout() time.Duration {
	budget := channelsRetentionMaxAttempts * channelsRetentionActivityTimeout
	interval := channelsRetentionRetryInterval
	for attempt := 1; attempt < channelsRetentionMaxAttempts; attempt++ {
		budget += interval
		interval *= channelsRetentionBackoffCoefficient
	}
	return budget + channelsRetentionSchedulingMargin
}

// AddChannelsRetentionSchedule installs the hourly sweep. The schedule ID names
// the task queue, so a preview owns its own copy and never re-points the
// shared one.
func AddChannelsRetentionSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	id := fmt.Sprintf("v1:channels-retention:%s", temporalEnv.Queue())
	sc := temporalEnv.Client().ScheduleClient()
	options := client.ScheduleOptions{
		ID:            id,
		CatchupWindow: channelsRetentionInterval - time.Second,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: channelsRetentionInterval}}},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           ChannelsRetentionWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: channelsRetentionRunTimeout(),
		},
	}
	_, err := sc.Create(ctx, options)
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		err = sc.GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				schedule := input.Description.Schedule
				action, ok := schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || action.TaskQueue != string(temporalEnv.Queue()) {
					return nil, errors.New("channels retention schedule is owned by another task queue")
				}
				if action.WorkflowRunTimeout == channelsRetentionRunTimeout() &&
					schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				// Preserve operator pause state while migrating limits.
				action.WorkflowRunTimeout = channelsRetentionRunTimeout()
				if schedule.Policy == nil {
					var policy client.SchedulePolicies
					schedule.Policy = &policy
				}
				schedule.Policy.CatchupWindow = options.CatchupWindow
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
		if err != nil {
			return fmt.Errorf("update channels retention schedule: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("create channels retention schedule: %w", err)
	}
	return nil
}
