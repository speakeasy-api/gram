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
	weeklyUsageSummaryScheduleID = "v1:weekly-usage-summary-schedule"
	weeklyUsageSummaryWorkflowID = "v1:weekly-usage-summary-schedule/scheduled"
	weeklyUsageSummaryRunTimeout = 30 * time.Minute
)

// WeeklyUsageSummaryWorkflow fans one usage summary email out to every
// organization with a billing alert contact. Per-org failures are logged and
// counted rather than failing the sweep, so one bad org cannot suppress
// everyone else's email; retries within a run are deduplicated by the send's
// Loops idempotency key, which is derived from the sweep's run time.
func WeeklyUsageSummaryWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	logger := workflow.GetLogger(ctx)
	runTime := workflow.Now(ctx).UTC()

	var a *Activities
	var targets []activities.WeeklyUsageSummaryTarget
	if err := workflow.ExecuteActivity(ctx, a.ListWeeklyUsageSummaryTargets).Get(ctx, &targets); err != nil {
		return fmt.Errorf("list weekly usage summary targets: %w", err)
	}

	failed := 0
	for _, target := range targets {
		if err := workflow.ExecuteActivity(ctx, a.SendWeeklyUsageSummary, activities.SendWeeklyUsageSummaryArgs{
			Target:  target,
			RunTime: runTime,
		}).Get(ctx, nil); err != nil {
			logger.Error("failed to send weekly usage summary", "organization_id", target.OrganizationID, "error", err)
			failed++
		}
	}

	if failed > 0 {
		logger.Warn("weekly usage summary sweep completed with failures", "failed_count", failed, "total_count", len(targets))
		return nil
	}

	logger.Info("weekly usage summary sweep completed", "total_count", len(targets))
	return nil
}

// AddWeeklyUsageSummarySchedule fires the sweep every Monday at 16:00 UTC —
// morning across US timezones, inside European working hours.
func AddWeeklyUsageSummarySchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	// A ScheduleRange with zero End/Step means exactly Start (End defaults to
	// Start, Step to 1); unset Second/Minute calendar fields default to 0.
	spec := client.ScheduleSpec{
		Calendars: []client.ScheduleCalendarSpec{
			{
				Second:     nil,
				Minute:     nil,
				Hour:       []client.ScheduleRange{{Start: 16, End: 0, Step: 0}},
				DayOfMonth: nil,
				Month:      nil,
				Year:       nil,
				DayOfWeek:  []client.ScheduleRange{{Start: 1, End: 0, Step: 0}}, // Monday
				Comment:    "weekly usage summary emails, Mondays 16:00 UTC",
			},
		},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 weeklyUsageSummaryWorkflowID,
		Workflow:           WeeklyUsageSummaryWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: weeklyUsageSummaryRunTimeout,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID:      weeklyUsageSummaryScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, weeklyUsageSummaryScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update weekly usage summary schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create weekly usage summary schedule: %w", err)
	}
	return nil
}
