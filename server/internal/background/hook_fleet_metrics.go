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

// HookFleetMetricsEmitter records the per-organization hooks fleet gauges.
type HookFleetMetricsEmitter interface {
	Emit(context.Context) error
}

const (
	hookFleetMetricsActivity       = "EmitHookFleetMetrics"
	hookFleetMetricsAttemptTimeout = 5 * time.Minute
	hookFleetMetricsMaxAttempts    = 3
	hookFleetMetricsRetryInterval  = time.Minute
	hookFleetMetricsRunTimeout     = 20 * time.Minute
	hookFleetMetricsCatchupWindow  = 12 * time.Hour
)

// HookFleetMetricsWorkflow emits the hooks fleet gauges with one activity.
func HookFleetMetricsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: hookFleetMetricsAttemptTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: hookFleetMetricsMaxAttempts,
			InitialInterval: hookFleetMetricsRetryInterval,
		},
	})
	if err := workflow.ExecuteActivity(ctx, hookFleetMetricsActivity).Get(ctx, nil); err != nil {
		return fmt.Errorf("emit hook fleet metrics: %w", err)
	}
	return nil
}

// addHookFleetMetricsSchedule emits the gauges daily at 06:00 UTC. Only the
// shared worker owns this fleet-wide schedule.
func addHookFleetMetricsSchedule(ctx context.Context, env *tenv.Environment) error {
	if env.Queue() != sharedTaskQueue {
		return nil
	}
	id := fmt.Sprintf("v1:hook-fleet-metrics:%s", env.Queue())
	_, err := env.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:   id,
		Spec: client.ScheduleSpec{CronExpressions: []string{"0 6 * * *"}, TimeZoneName: "UTC"},
		Action: &client.ScheduleWorkflowAction{
			ID:                 id + "/scheduled",
			Workflow:           HookFleetMetricsWorkflow,
			TaskQueue:          string(env.Queue()),
			WorkflowRunTimeout: hookFleetMetricsRunTimeout,
		},
		Overlap:            enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow:      hookFleetMetricsCatchupWindow,
		TriggerImmediately: true,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create hook fleet metrics schedule: %w", err)
	}
	return nil
}
