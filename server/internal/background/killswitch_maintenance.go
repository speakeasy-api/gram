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

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The killswitch maintenance sweep records history only: query-time database
// state is authoritative for enforcement, so a delayed or absent sweep never
// extends or shortens any prescription. Workflow inputs and results carry no
// organization, prescription, operation, or note data — only batch sizes in
// and aggregate counts out.
const (
	killswitchMaintenanceScheduleID       = "v1:killswitch-maintenance-schedule"
	killswitchMaintenanceWorkflowID       = killswitchMaintenanceScheduleID + "/scheduled"
	killswitchMaintenanceInterval         = 5 * time.Minute
	killswitchExpiryBatchSize       int32 = 100
	killswitchCleanupBatchSize      int32 = 500
)

func KillswitchMaintenanceWorkflow(ctx workflow.Context) error {
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

	// Expiry recording and receipt cleanup run as separate activities with
	// separate database transactions, so a cleanup failure cannot roll back or
	// block recorded expiry history.
	for {
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, KillswitchMaintenanceWorkflow)
		}

		var batch killswitches.ExpiryBatchResult
		if err := workflow.ExecuteActivity(ctx, a.RecordDueKillswitchExpiries, killswitchExpiryBatchSize).Get(ctx, &batch); err != nil {
			return fmt.Errorf("record due killswitch expiries: %w", err)
		}
		workflow.GetLogger(ctx).Info("killswitch expiry batch completed", "candidates", batch.Candidates, "rows_recorded", batch.Recorded)
		// Drain on the candidate count: a full candidate batch can still record
		// fewer rows when candidates raced with a concurrent sweep, and more due
		// work may remain behind it.
		if batch.Candidates < int64(killswitchExpiryBatchSize) {
			break
		}
	}

	for {
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, KillswitchMaintenanceWorkflow)
		}

		var deleted int64
		if err := workflow.ExecuteActivity(ctx, a.CleanupExpiredKillswitchOperations, killswitchCleanupBatchSize).Get(ctx, &deleted); err != nil {
			return fmt.Errorf("cleanup expired killswitch operations: %w", err)
		}
		workflow.GetLogger(ctx).Info("killswitch operation cleanup batch completed", "rows_deleted", deleted)
		if deleted < int64(killswitchCleanupBatchSize) {
			return nil
		}
	}
}

func AddKillswitchMaintenanceSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: killswitchMaintenanceInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 killswitchMaintenanceWorkflowID,
		Workflow:           KillswitchMaintenanceWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 5 * time.Minute,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      killswitchMaintenanceScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, killswitchMaintenanceScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing killswitch maintenance schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create killswitch maintenance schedule: %w", err)
	}

	return nil
}
