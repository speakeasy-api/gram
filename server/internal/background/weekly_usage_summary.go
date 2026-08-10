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
	// End-to-end budget for one activity: queue delay plus every retry
	// attempt (3 × 2m StartToClose with 10s+20s backoff and Temporal
	// jitter). Enforced as ScheduleToCloseTimeout — so a queued or retrying
	// activity cannot outlive it — and reserved as headroom by the
	// ContinueAsNew guard, so a run never starts an activity it cannot see
	// finish within the run timeout.
	weeklyUsageSummaryActivityBudget = 7 * time.Minute
)

// WeeklyUsageSummaryInput carries the sweep's state across ContinueAsNew
// runs. The zero value is the schedule's entry point: targets are resolved
// and the run time is anchored on the first run, then both are carried
// forward so cycle math and idempotency keys stay stable for the whole
// sweep.
type WeeklyUsageSummaryInput struct {
	Targets     []activities.WeeklyUsageSummaryTarget
	StartIndex  int
	FailedCount int
	RunTime     time.Time
}

// WeeklyUsageSummaryWorkflow fans one usage summary email out to every
// organization with a billing alert contact. Per-org failures are logged and
// counted rather than failing the sweep, so one bad org cannot suppress
// everyone else's email; retries within a sweep are deduplicated by the
// send's Loops idempotency key, which is derived from the sweep's run time.
// When the run approaches its timeout — e.g. many failing sends each burning
// their full retry budget — it continues as new from the next unprocessed
// target instead of being terminated mid-sweep.
func WeeklyUsageSummaryWorkflow(ctx workflow.Context, input WeeklyUsageSummaryInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    2 * time.Minute,
		ScheduleToCloseTimeout: weeklyUsageSummaryActivityBudget,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	logger := workflow.GetLogger(ctx)

	runTime := input.RunTime
	if runTime.IsZero() {
		runTime = workflow.Now(ctx).UTC()
	}

	var a *Activities
	targets := input.Targets
	if len(targets) == 0 {
		if err := workflow.ExecuteActivity(ctx, a.ListWeeklyUsageSummaryTargets).Get(ctx, &targets); err != nil {
			return fmt.Errorf("list weekly usage summary targets: %w", err)
		}
	}

	failed := input.FailedCount
	for i := min(max(input.StartIndex, 0), len(targets)); i < len(targets); i++ {
		target := targets[i]
		if err := workflow.ExecuteActivity(ctx, a.SendWeeklyUsageSummary, activities.SendWeeklyUsageSummaryArgs{
			Target:  target,
			RunTime: runTime,
		}).Get(ctx, nil); err != nil {
			logger.Error("failed to send weekly usage summary", "organization_id", target.OrganizationID, "error", err)
			failed++
		}

		if i+1 < len(targets) && shouldContinueWeeklyUsageSummaryAsNew(ctx) {
			logger.Info(
				"continuing weekly usage summary sweep as new",
				"next_start_index", i+1,
				"total_count", len(targets),
				"failed_count", failed,
			)
			return workflow.NewContinueAsNewError(ctx, WeeklyUsageSummaryWorkflow, WeeklyUsageSummaryInput{
				Targets:     targets,
				StartIndex:  i + 1,
				FailedCount: failed,
				RunTime:     runTime,
			})
		}
	}

	if failed > 0 {
		logger.Warn("weekly usage summary sweep completed with failures", "failed_count", failed, "total_count", len(targets))
		return nil
	}

	logger.Info("weekly usage summary sweep completed", "total_count", len(targets))
	return nil
}

// shouldContinueWeeklyUsageSummaryAsNew reports whether the sweep should
// hand off to a fresh run: either Temporal suggests it (history growth) or
// the elapsed run time leaves less than one activity budget before the run
// timeout.
func shouldContinueWeeklyUsageSummaryAsNew(ctx workflow.Context) bool {
	info := workflow.GetInfo(ctx)
	if info.GetContinueAsNewSuggested() {
		return true
	}

	runTimeout := info.WorkflowRunTimeout
	if runTimeout == 0 {
		runTimeout = weeklyUsageSummaryRunTimeout
	}
	if info.WorkflowStartTime.IsZero() {
		return false
	}

	elapsed := workflow.Now(ctx).Sub(info.WorkflowStartTime)
	return elapsed+weeklyUsageSummaryActivityBudget >= runTimeout
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
		ID:        weeklyUsageSummaryWorkflowID,
		Workflow:  WeeklyUsageSummaryWorkflow,
		Args:      []any{WeeklyUsageSummaryInput{Targets: nil, StartIndex: 0, FailedCount: 0, RunTime: time.Time{}}},
		TaskQueue: string(temporalEnv.Queue()),
		// The run timeout bounds a single run, not the sweep: the workflow
		// continues as new when it gets close, so a long tail of retrying
		// sends rolls into fresh runs instead of being cut off.
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
