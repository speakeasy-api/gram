package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	relay "github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

type ProcessOutboxResult struct {
}

func ProcessOutboxWorkflow(ctx workflow.Context) (ProcessOutboxResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	})

	var a *Activities

	for {
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return ProcessOutboxResult{}, workflow.NewContinueAsNewError(ctx, ProcessOutboxWorkflow)
		}

		var result relay.FetchEventsResult
		if err := workflow.ExecuteActivity(ctx, a.FetchPendingOutboxEvents, relay.FetchEventArgs{}).Get(ctx, &result); err != nil {
			return ProcessOutboxResult{}, fmt.Errorf("fetch pending outbox batch: %w", err)
		}

		if len(result.Events) > 0 {
			var filtered []*relay.Event
			if err := workflow.ExecuteActivity(ctx, a.FilterNoopOutboxEvents, result.Events).Get(ctx, &filtered); err != nil {
				return ProcessOutboxResult{}, fmt.Errorf("mark outbox svix events noop: %w", err)
			}

			if err := workflow.ExecuteActivity(ctx, a.RelayOutboxEvents, filtered).Get(ctx, nil); err != nil {
				return ProcessOutboxResult{}, fmt.Errorf("relay svix events: %w", err)
			}

			if result.HasMore {
				continue // more events waiting — poll immediately without sleeping
			}
		}

		if err := workflow.Sleep(ctx, 5*time.Second); err != nil {
			return ProcessOutboxResult{}, err
		}
	}
}

func AddProcessOutboxSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleID := "v1:process-outbox-schedule"
	workflowID := scheduleID + "/scheduled"

	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      scheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		// The schedule acts as a watchdog: the workflow polls internally every
		// 5 s and runs continuously until ContinueAsNew. This 1-minute interval
		// only restarts the workflow if it exits or exhausts its retry budget.
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{
					Every: 1 * time.Minute,
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        workflowID,
			Workflow:  ProcessOutboxWorkflow,
			TaskQueue: string(temporalEnv.Queue()),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create process outbox schedule: %w", err)
	}

	return nil
}
