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
	outboxGCScheduleID            = "v1:outbox-gc-schedule"
	outboxGCWorkflowID            = outboxGCScheduleID + "/scheduled"
	outboxGCInterval              = 5 * time.Minute
	outboxGCRetentionPeriod       = 7 * 24 * time.Hour
	outboxGCBatchSize       int32 = 100
)

func OutboxGCWorkflow(ctx workflow.Context) error {
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
			return workflow.NewContinueAsNewError(ctx, OutboxGCWorkflow)
		}

		cutoff := workflow.Now(ctx).Add(-outboxGCRetentionPeriod)

		var rows int64
		if err := workflow.ExecuteActivity(ctx, a.GCOutboxProcessedRows, cutoff, outboxGCBatchSize).Get(ctx, &rows); err != nil {
			return fmt.Errorf("gc outbox processed rows: %w", err)
		}

		workflow.GetLogger(ctx).Info("outbox gc batch completed", "rows_deleted", rows)

		if rows < int64(outboxGCBatchSize) {
			return nil // all rows processed — schedule will re-run at next interval
		}
	}
}

func AddOutboxGCSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: outboxGCInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 outboxGCWorkflowID,
		Workflow:           OutboxGCWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 2 * time.Minute,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      outboxGCScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, outboxGCScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing outbox gc schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create outbox gc schedule: %w", err)
	}

	return nil
}
