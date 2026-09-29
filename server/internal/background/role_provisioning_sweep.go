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

	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const roleProvisioningSweepInterval = time.Hour

// RoleProvisioningSweepWorkflow executes one database-only activity per hourly
// global tick. Budget: 24 * 30 * (1 workflow start + 1 activity) = approximately
// 1,440 base Temporal actions/month per namespace/task queue, plus retries.
// Tenant/role fanout runs through the bounded Pub/Sub consumer, never Temporal.
func RoleProvisioningSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second, ScheduleToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3, InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 10 * time.Second},
	})
	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.RequestRoleProvisioningSweep).Get(ctx, nil); err != nil {
		return fmt.Errorf("request role provisioning sweep: %w", err)
	}
	return nil
}

func (a *Activities) RequestRoleProvisioningSweep(ctx context.Context) error {
	if err := roleprovisioning.RequestSweep(ctx, a.db); err != nil {
		return fmt.Errorf("request global role maintenance: %w", err)
	}
	return nil
}

func roleProvisioningSweepScheduleID(queue string) string {
	return fmt.Sprintf("v1:role-provisioning-sweep:%s", queue)
}

func roleProvisioningSweepScheduleOptions(queue string) client.ScheduleOptions {
	id := roleProvisioningSweepScheduleID(queue)
	return client.ScheduleOptions{
		ID: id, CatchupWindow: roleProvisioningSweepInterval - time.Second, Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:   client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: roleProvisioningSweepInterval}}, Jitter: 5 * time.Minute},
		Action: &client.ScheduleWorkflowAction{ID: id + "/scheduled", Workflow: RoleProvisioningSweepWorkflow, TaskQueue: queue, WorkflowExecutionTimeout: 3 * time.Minute},
	}
}

func addRoleProvisioningSweepSchedule(ctx context.Context, env *tenv.Environment) error {
	options := roleProvisioningSweepScheduleOptions(string(env.Queue()))
	sc := env.Client().ScheduleClient()
	if _, err := sc.Create(ctx, options); err == nil {
		return nil
	} else if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create role provisioning sweep: %w", err)
	}
	handle := sc.GetHandle(ctx, options.ID)
	existing, err := handle.Describe(ctx)
	if err != nil {
		return fmt.Errorf("describe role provisioning sweep: %w", err)
	}
	// Do not update an unchanged schedule at every worker startup. Policy repair
	// preserves operator pauses and never takes ownership of another task queue.
	if existing.Schedule.Policy != nil && existing.Schedule.Policy.CatchupWindow == options.CatchupWindow {
		return nil
	}
	if err := handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			current, ok := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction)
			if !ok || current.TaskQueue != string(env.Queue()) {
				return nil, fmt.Errorf("role provisioning sweep is owned by another task queue")
			}
			setScheduleCatchup(&input.Description.Schedule, options.CatchupWindow)
			return &client.ScheduleUpdate{Schedule: &input.Description.Schedule, TypedSearchAttributes: nil}, nil
		},
	}); err != nil {
		return fmt.Errorf("repair role provisioning sweep policy: %w", err)
	}
	return nil
}
