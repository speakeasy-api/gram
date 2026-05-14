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
	outboxGCInterval              = 6 * time.Hour
	outboxGCRetentionPeriod       = 7 * 24 * time.Hour
	outboxGCBatchSize       int32 = 100
	outboxGCSleepInterval         = time.Hour
)

func OutboxGCWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
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

		if rows >= int64(outboxGCBatchSize) {
			continue // batch was full — more rows likely remain
		}

		if err := workflow.Sleep(ctx, outboxGCSleepInterval); err != nil {
			return err
		}
	}
}

func AddOutboxGCSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	_, err := temporalEnv.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:      outboxGCScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: outboxGCInterval}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 outboxGCWorkflowID,
			Workflow:           OutboxGCWorkflow,
			TaskQueue:          string(temporalEnv.Queue()),
			WorkflowRunTimeout: 15 * time.Minute,
		},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create outbox gc schedule: %w", err)
	}
	return nil
}
