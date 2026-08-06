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

const (
	publishOutboxGCScheduleID = "v1:publish-outbox-gc-schedule"
	publishOutboxGCWorkflowID = publishOutboxGCScheduleID + "/scheduled"
	publishOutboxGCInterval   = 1 * time.Hour
	// Dead letters are kept long enough to be noticed and replayed. The queue
	// table itself needs no GC — rows are deleted as they publish.
	publishOutboxGCRetentionPeriod       = 30 * 24 * time.Hour
	publishOutboxGCBatchSize       int32 = 100
)

// PublishOutboxGCWorkflow bounds the publish outbox dead letter table.
func PublishOutboxGCWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities

	for {
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, PublishOutboxGCWorkflow)
		}

		cutoff := workflow.Now(ctx).Add(-publishOutboxGCRetentionPeriod)

		var rows int64
		if err := workflow.ExecuteActivity(ctx, a.GCPublishOutboxDeadLetters, cutoff, publishOutboxGCBatchSize).Get(ctx, &rows); err != nil {
			return fmt.Errorf("gc publish outbox dead letters: %w", err)
		}

		workflow.GetLogger(ctx).Info("publish outbox gc batch completed", "rows_deleted", rows)

		if rows < int64(publishOutboxGCBatchSize) {
			return nil // all rows processed — schedule will re-run at next interval
		}
	}
}

func AddPublishOutboxGCSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: publishOutboxGCInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 publishOutboxGCWorkflowID,
		Workflow:           PublishOutboxGCWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 5 * time.Minute,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      publishOutboxGCScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, publishOutboxGCScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing publish outbox gc schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create publish outbox gc schedule: %w", err)
	}

	return nil
}
