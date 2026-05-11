package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	relay "github.com/speakeasy-api/gram/server/internal/background/activities/outbox_svix_relay"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

type RelayOutboxSvixResult struct {
}

func RelayOutboxToSvixWorkflow(ctx workflow.Context) (RelayOutboxSvixResult, error) {
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
			return RelayOutboxSvixResult{}, workflow.NewContinueAsNewError(ctx, RelayOutboxToSvixWorkflow)
		}

		var result relay.FetchEventsResult
		if err := workflow.ExecuteActivity(ctx, a.FetchPendingSvixEvents, relay.FetchEventArgs{}).Get(ctx, &result); err != nil {
			return RelayOutboxSvixResult{}, fmt.Errorf("fetch pending outbox batch: %w", err)
		}

		if len(result.Events) > 0 {
			var filtered []*relay.OutboxSvixEvent
			if err := workflow.ExecuteActivity(ctx, a.FilterNoopSvixEvents, result.Events).Get(ctx, &filtered); err != nil {
				return RelayOutboxSvixResult{}, fmt.Errorf("mark outbox svix events noop: %w", err)
			}

			if err := workflow.ExecuteActivity(ctx, a.RelaySvixEvents, filtered).Get(ctx, nil); err != nil {
				return RelayOutboxSvixResult{}, fmt.Errorf("relay svix events: %w", err)
			}

			if result.HasMore {
				continue // more events waiting — poll immediately without sleeping
			}
		}

		if err := workflow.Sleep(ctx, 5*time.Second); err != nil {
			return RelayOutboxSvixResult{}, err
		}
	}
}

func AddRelayOutboxToSvixSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	workflowID := "v1:relay-outbox-svix-schedule"
	scheduleID := "v1:relay-outbox-svix-schedule"

	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      scheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{
					Every: 1 * time.Minute,
				},
			},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        workflowID,
			Workflow:  RelayOutboxToSvixWorkflow,
			TaskQueue: string(temporalEnv.Queue()),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create relay outbox svix schedule: %w", err)
	}

	return nil
}
