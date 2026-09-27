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
	networkIngressSweepScheduleIDPrefix = "v1:network-ingress-reconcile-sweep:"
	networkIngressSweepInterval         = time.Hour

	// Allow lateness up to one interval minus 1s; skip older missed ticks.
	networkIngressSweepCatchupWindow = networkIngressSweepInterval - time.Second

	networkIngressSweepJitter           = 5 * time.Minute
	networkIngressSweepActivityTimeout  = 5 * time.Minute
	networkIngressSweepActivityAttempts = 3
	// Three full attempts plus the 10s and 20s exponential backoffs.
	networkIngressSweepActivityRetryBudget = networkIngressSweepActivityAttempts*networkIngressSweepActivityTimeout + 30*time.Second
	// Leave room for workflow tasks and activity scheduling around both budgets.
	networkIngressSweepWorkflowOverhead = 5 * time.Minute
	networkIngressSweepExecutionTimeout = 2*networkIngressSweepActivityRetryBudget + networkIngressSweepWorkflowOverhead
)

func NetworkIngressSweepWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    networkIngressSweepActivityTimeout,
		ScheduleToCloseTimeout: networkIngressSweepActivityRetryBudget,
		RetryPolicy:            &temporal.RetryPolicy{InitialInterval: 10 * time.Second, MaximumInterval: time.Minute, MaximumAttempts: networkIngressSweepActivityAttempts},
	})
	if err := workflow.ExecuteActivity(ctx, "SweepNetworkIngresses").Get(ctx, nil); err != nil {
		return fmt.Errorf("sweep network ingresses: %w", err)
	}
	if err := workflow.ExecuteActivity(ctx, "FindNetworkIngressOrphans").Get(ctx, nil); err != nil {
		return fmt.Errorf("inventory network ingress orphans: %w", err)
	}
	return nil
}

func buildNetworkIngressSweepScheduleOptions(namespace, taskQueue string) client.ScheduleOptions {
	id := networkIngressSweepScheduleIDPrefix + namespace
	return client.ScheduleOptions{
		CatchupWindow: networkIngressSweepCatchupWindow,
		ID:            id,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: networkIngressSweepInterval}},
			Jitter:    networkIngressSweepJitter,
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                       id + "/scheduled",
			Workflow:                 NetworkIngressSweepWorkflow,
			TaskQueue:                taskQueue,
			WorkflowExecutionTimeout: networkIngressSweepExecutionTimeout,
		},
	}
}

func addNetworkIngressSweep(ctx context.Context, env *tenv.Environment) error {
	sc := env.Client().ScheduleClient()
	options := buildNetworkIngressSweepScheduleOptions(string(env.Namespace()), string(env.Queue()))

	_, err := sc.Create(ctx, options)
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, options.ID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				current, ok := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || current.TaskQueue != string(env.Queue()) {
					return nil, fmt.Errorf("network ingress sweep is owned by another task queue")
				}
				input.Description.Schedule.Spec = &options.Spec
				input.Description.Schedule.Action = options.Action
				input.Description.Schedule.Policy = &client.SchedulePolicies{Overlap: options.Overlap, CatchupWindow: networkIngressSweepCatchupWindow, PauseOnFailure: false}
				setScheduleCatchup(&input.Description.Schedule, networkIngressSweepCatchupWindow)
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing network ingress sweep: %w", err)
		}
	case err != nil:
		return fmt.Errorf("register network ingress sweep: %w", err)
	}
	return nil
}
