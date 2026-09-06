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

const tenantDimensionsSyncInterval = 15 * time.Minute

// SyncTenantDimensionsWorkflow refreshes the ClickHouse organization and
// project reporting dimensions from one consistent Postgres snapshot.
func SyncTenantDimensionsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    2 * time.Minute,
		ScheduleToCloseTimeout: 8 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.SyncTenantDimensions).Get(ctx, nil); err != nil {
		return fmt.Errorf("sync tenant dimensions: %w", err)
	}
	return nil
}

// AddTenantDimensionsSyncSchedule installs the queue-scoped periodic dimension
// refresh. Queue scoping prevents preview workers sharing one namespace from
// repointing or deleting another worktree's schedule.
func AddTenantDimensionsSyncSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	queue := string(temporalEnv.Queue())
	scheduleID := fmt.Sprintf("v1:tenant-dimensions-sync:%s", queue)
	workflowID := scheduleID + "/scheduled"
	scheduleClient := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: tenantDimensionsSyncInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 workflowID,
		Workflow:           SyncTenantDimensionsWorkflow,
		TaskQueue:          queue,
		WorkflowRunTimeout: 10 * time.Minute,
	}

	_, err := scheduleClient.Create(ctx, client.ScheduleOptions{
		ID:      scheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, scheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update tenant dimension sync schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create tenant dimension sync schedule: %w", err)
	}
	return nil
}
