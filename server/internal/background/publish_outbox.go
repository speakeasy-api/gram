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

	relay "github.com/speakeasy-api/gram/server/internal/background/activities/publish_outbox"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	publishOutboxScheduleID = "v1:publish-outbox-schedule"
	publishOutboxWorkflowID = publishOutboxScheduleID + "/scheduled"
	// The schedule is a watchdog, not the poll loop. The workflow polls
	// internally and runs continuously until ContinueAsNew; this interval only
	// restarts it if it exits or exhausts its retry budget.
	publishOutboxWatchdogInterval = 1 * time.Minute
	publishOutboxIdleInterval     = 5 * time.Second
	// publishOutboxDrainTimeout bounds one batch. The publish phase cannot
	// outlast the 30s PublishSettings.Timeout the outbox publisher is built with
	// (see deps.go), which leaves the claim and the settlement statements; 90s is
	// that with room to spare. It sits deliberately above the relay's 60s claim
	// lease: an attempt that reaches this bound has certainly lost its claim by
	// the time the retry starts, so the retry re-claims immediately instead of
	// racing the lease out.
	publishOutboxDrainTimeout = 90 * time.Second
)

type PublishOutboxResult struct{}

// PublishOutboxWorkflow drains the publish outbox onto Pub/Sub.
//
// A single activity does the whole batch — claim, publish, settle — so no
// message body crosses the activity boundary and lands in workflow history.
func PublishOutboxWorkflow(ctx workflow.Context) (PublishOutboxResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: publishOutboxDrainTimeout,
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
			return PublishOutboxResult{}, workflow.NewContinueAsNewError(ctx, PublishOutboxWorkflow)
		}

		var result relay.DrainResult
		if err := workflow.ExecuteActivity(ctx, a.DrainPublishOutbox).Get(ctx, &result); err != nil {
			return PublishOutboxResult{}, fmt.Errorf("drain publish outbox: %w", err)
		}

		if result.HasMore {
			continue // more rows waiting — poll immediately without sleeping
		}

		if err := workflow.Sleep(ctx, publishOutboxIdleInterval); err != nil {
			return PublishOutboxResult{}, fmt.Errorf("sleep between publish outbox drains: %w", err)
		}
	}
}

func AddPublishOutboxSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: publishOutboxWatchdogInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:        publishOutboxWorkflowID,
		Workflow:  PublishOutboxWorkflow,
		TaskQueue: string(temporalEnv.Queue()),
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      publishOutboxScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		// Creating is not enough on its own: an existing schedule keeps whatever
		// spec it was created with, so a changed interval or task queue would
		// never take effect without this update.
		if err := sc.GetHandle(ctx, publishOutboxScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing publish outbox schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create publish outbox schedule: %w", err)
	}

	return nil
}
