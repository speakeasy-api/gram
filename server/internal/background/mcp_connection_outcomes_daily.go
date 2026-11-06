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
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/dailyoutcomes"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	mcpConnectionOutcomesScheduleID      = "v1:mcp-connection-outcomes-daily-schedule"
	mcpConnectionOutcomesSweepWorkflowID = mcpConnectionOutcomesScheduleID + "/scheduled"

	// The run starts at 01:30 UTC, when dailyoutcomes.DayCloseDelay has closed yesterday.
	mcpConnectionOutcomesHourUTC   = 1
	mcpConnectionOutcomesMinuteUTC = 30

	// A missed run is caught up within a day. The plan also rebuilds any day the
	// table lacks, so a longer outage loses nothing either.
	mcpConnectionOutcomesCatchupWindow = 23 * time.Hour

	// mcpConnectionOutcomesConcurrency bounds how many days build at once, so a
	// first-deploy backfill of about 28 days does not load ClickHouse all at once.
	mcpConnectionOutcomesConcurrency = 4

	// A day reads up to nine days of raw events and writes a few hundred rows; real
	// builds take seconds. The timeout leaves room for a slow ClickHouse.
	mcpConnectionOutcomesBuildTimeout     = 20 * time.Minute
	mcpConnectionOutcomesBuildMaxAttempts = 3
	mcpConnectionOutcomesDayRunTimeout    = mcpConnectionOutcomesBuildMaxAttempts*mcpConnectionOutcomesBuildTimeout + 10*time.Minute

	// A backfill runs about seven waves of days one after another.
	mcpConnectionOutcomesSweepRunTimeout = 8 * time.Hour
)

// MCPConnectionOutcomesDayParams names the UTC day to build.
type MCPConnectionOutcomesDayParams struct {
	Day time.Time
}

// mcpConnectionOutcomesDayWorkflowID is one per day, so two builds of a day never overlap.
func mcpConnectionOutcomesDayWorkflowID(day time.Time) string {
	return "v1:mcp-connection-outcomes-daily:" + dailyoutcomes.StartOfDay(day).Format(time.DateOnly)
}

// MCPConnectionOutcomesSweepWorkflow builds every day that needs it: closed days whose
// recovery window is still open, and any day with raw events the long-term table lacks.
// The first run after deploy backfills every day since raw events began.
//
// Temporal actions/month ≈ 30 starts + 30 plan activities + ~240 day starts + ~240 build
// activities ≈ 540 per namespace (8 days rebuilt per run while their recovery window is
// open), plus a one-time backfill of about 60. Fixed: one fleet-wide schedule.
func MCPConnectionOutcomesSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities
	var days []time.Time
	if err := workflow.ExecuteActivity(ctx, a.PlanMCPConnectionOutcomeDays, activities.PlanMCPConnectionOutcomeDaysArgs{
		Now: workflow.Now(ctx).UTC(),
	}).Get(ctx, &days); err != nil {
		return fmt.Errorf("plan connection outcome days: %w", err)
	}

	logger := workflow.GetLogger(ctx)
	var errs []error
	for start := 0; start < len(days); start += mcpConnectionOutcomesConcurrency {
		wave := days[start:min(start+mcpConnectionOutcomesConcurrency, len(days))]
		futures := make([]workflow.ChildWorkflowFuture, 0, len(wave))
		for _, day := range wave {
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:            mcpConnectionOutcomesDayWorkflowID(day),
				WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
				WorkflowRunTimeout:    mcpConnectionOutcomesDayRunTimeout,
				ParentClosePolicy:     enums.PARENT_CLOSE_POLICY_ABANDON,
			})
			futures = append(futures, workflow.ExecuteChildWorkflow(childCtx, MCPConnectionOutcomesDayWorkflow, MCPConnectionOutcomesDayParams{Day: day}))
		}
		for i, future := range futures {
			day := wave[i].Format(time.DateOnly)
			if err := future.GetChildWorkflowExecution().Get(ctx, nil); err != nil {
				// A build of this day is still running from an earlier sweep.
				logger.Info("connection outcome day already building or failed to start", "day", day, "error", err.Error())
				continue
			}
			if err := future.Get(ctx, nil); err != nil {
				errs = append(errs, fmt.Errorf("build connection outcomes for %s: %w", day, err))
			}
		}
	}
	return errors.Join(errs...)
}

// MCPConnectionOutcomesDayWorkflow rebuilds one day. The build is one activity, safe
// to retry from the start: it clears staging first and changes the stored day only
// through one REPLACE PARTITION.
func MCPConnectionOutcomesDayWorkflow(ctx workflow.Context, params MCPConnectionOutcomesDayParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: mcpConnectionOutcomesBuildTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    mcpConnectionOutcomesBuildMaxAttempts,
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.BuildMCPConnectionOutcomesDay, activities.BuildMCPConnectionOutcomesDayArgs{
		Day: params.Day,
		Now: workflow.Now(ctx).UTC(),
	}).Get(ctx, nil); err != nil {
		return fmt.Errorf("build connection outcomes day: %w", err)
	}
	return nil
}

func mcpConnectionOutcomesScheduleOptions(taskQueue string) client.ScheduleOptions {
	return client.ScheduleOptions{
		CatchupWindow: mcpConnectionOutcomesCatchupWindow,
		ID:            mcpConnectionOutcomesScheduleID,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Calendars: []client.ScheduleCalendarSpec{{
				Second:     nil,
				Minute:     []client.ScheduleRange{{Start: mcpConnectionOutcomesMinuteUTC, End: 0, Step: 0}},
				Hour:       []client.ScheduleRange{{Start: mcpConnectionOutcomesHourUTC, End: 0, Step: 0}},
				DayOfMonth: nil,
				Month:      nil,
				Year:       nil,
				DayOfWeek:  nil,
				Comment:    "build MCP connection outcomes at 01:30 UTC",
			}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 mcpConnectionOutcomesSweepWorkflowID,
			Workflow:           MCPConnectionOutcomesSweepWorkflow,
			TaskQueue:          taskQueue,
			WorkflowRunTimeout: mcpConnectionOutcomesSweepRunTimeout,
		},
	}
}

// AddMCPConnectionOutcomesSchedule installs the daily schedule. A new schedule runs
// once right away, so a first deploy backfills without waiting for 01:30.
func AddMCPConnectionOutcomesSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()
	options := mcpConnectionOutcomesScheduleOptions(string(temporalEnv.Queue()))

	handle, err := sc.Create(ctx, options)
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, mcpConnectionOutcomesScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &options.Spec
				input.Description.Schedule.Action = options.Action
				setScheduleCatchup(&input.Description.Schedule, mcpConnectionOutcomesCatchupWindow)
				return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
			},
		}); err != nil {
			return fmt.Errorf("update MCP connection outcomes schedule: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("create MCP connection outcomes schedule: %w", err)
	}

	if err := handle.Trigger(ctx, client.ScheduleTriggerOptions{Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP}); err != nil {
		return fmt.Errorf("run MCP connection outcomes schedule once: %w", err)
	}
	return nil
}
