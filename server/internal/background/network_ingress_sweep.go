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

func NetworkIngressSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: 10 * time.Second, MaximumInterval: time.Minute, MaximumAttempts: 3},
	})
	if err := workflow.ExecuteActivity(ctx, "SweepNetworkIngresses").Get(ctx, nil); err != nil {
		return fmt.Errorf("sweep network ingresses: %w", err)
	}
	if err := workflow.ExecuteActivity(ctx, "FindNetworkIngressOrphans").Get(ctx, nil); err != nil {
		return fmt.Errorf("inventory network ingress orphans: %w", err)
	}
	return nil
}

func addNetworkIngressSweep(ctx context.Context, env *tenv.Environment) error {
	id := "v1:network-ingress-reconcile-sweep:" + string(env.Queue())
	_, err := env.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: id, Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:   client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}, Jitter: 5 * time.Minute},
		Action: &client.ScheduleWorkflowAction{ID: id + "/scheduled", Workflow: NetworkIngressSweepWorkflow, TaskQueue: string(env.Queue()), WorkflowExecutionTimeout: 20 * time.Minute},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("register network ingress sweep: %w", err)
	}
	return nil
}
