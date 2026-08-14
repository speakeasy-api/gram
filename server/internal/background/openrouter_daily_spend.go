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
	openRouterDailySpendScheduleID          = "v1:collect-openrouter-daily-spend-schedule"
	openRouterDailySpendScheduledWorkflowID = "v1:collect-openrouter-daily-spend/scheduled"

	openRouterDailySpendLookbackDays        = 3
	openRouterDailySpendActivityMaxAttempts = 3
	openRouterDailySpendActivityTimeout     = 2 * time.Hour
	// Schedule-to-close covers all three two-hour attempts, retry backoff, and
	// queueing delay. The workflow gets a further hour to record the result.
	openRouterDailySpendActivityScheduleToCloseTimeout = 7 * time.Hour
	openRouterDailySpendWorkflowRunTimeout             = openRouterDailySpendActivityScheduleToCloseTimeout + time.Hour
)

func CollectOpenRouterDailySpendWorkflow(ctx workflow.Context) error {
	now := workflow.Now(ctx).UTC()
	endDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startDay := endDay.AddDate(0, 0, -openRouterDailySpendLookbackDays)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    openRouterDailySpendActivityTimeout,
		ScheduleToCloseTimeout: openRouterDailySpendActivityScheduleToCloseTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    openRouterDailySpendActivityMaxAttempts,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.CollectOpenRouterDailySpend, activities.CollectOpenRouterDailySpendArgs{
		StartDay: startDay,
		EndDay:   endDay,
	}).Get(ctx, nil); err != nil {
		return fmt.Errorf("collect openrouter daily spend: %w", err)
	}

	return nil
}

func openRouterDailySpendScheduleOptions(taskQueue string) client.ScheduleOptions {
	return client.ScheduleOptions{
		ID:      openRouterDailySpendScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Calendars: []client.ScheduleCalendarSpec{
				{
					Second:     nil,
					Minute:     nil,
					Hour:       []client.ScheduleRange{{Start: 4, End: 0, Step: 0}},
					DayOfMonth: nil,
					Month:      nil,
					Year:       nil,
					DayOfWeek:  nil,
					Comment:    "collect OpenRouter daily spend at 04:00 UTC",
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 openRouterDailySpendScheduledWorkflowID,
			Workflow:           CollectOpenRouterDailySpendWorkflow,
			TaskQueue:          taskQueue,
			WorkflowRunTimeout: openRouterDailySpendWorkflowRunTimeout,
		},
	}
}

func AddOpenRouterDailySpendSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := openRouterDailySpendScheduleOptions(string(temporalEnv.Queue()))

	_, err := scheduleClient.Create(ctx, options)
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, openRouterDailySpendScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &options.Spec
				input.Description.Schedule.Action = options.Action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing openrouter daily spend schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create openrouter daily spend schedule: %w", err)
	}

	return nil
}
