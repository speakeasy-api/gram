package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const NetworkIngressReconcileActivityName = "ReconcileNetworkIngress"

// NetworkIngressReconcileParams deliberately excludes provider state and secrets.
type NetworkIngressReconcileParams struct {
	// IngressID selects current authoritative state, including a tombstone.
	IngressID uuid.UUID `json:"ingress_id"`
}

// NetworkIngressReconcileResult asks for a new pass only on a concurrent edit.
type NetworkIngressReconcileResult struct {
	// Requeue denotes newer desired state, not provider convergence pending.
	Requeue bool `json:"requeue"`
}

// NetworkIngressClient targets only the explicitly configured lifecycle queue.
type NetworkIngressClient struct {
	// Client connects to the deployment's Temporal namespace.
	Client client.Client
	// Queue is the authoritative reconciliation worker queue.
	Queue string
}

func (c *NetworkIngressClient) start(ctx context.Context, id uuid.UUID) (client.WorkflowRun, error) {
	if c.Client == nil || c.Queue == "" || id == uuid.Nil {
		return nil, fmt.Errorf("network ingress reconciliation is unconfigured")
	}
	startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	workflowID := "v1:network-ingress-reconcile:" + c.Queue + ":" + id.String()
	run, err := c.Client.SignalWithStartWorkflow(startCtx, workflowID, "reconcile", "enqueue", client.StartWorkflowOptions{
		ID: workflowID, TaskQueue: c.Queue,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowExecutionTimeout: 30 * time.Minute,
		WorkflowRunTimeout:       10 * time.Minute,
	}, NetworkIngressReconcileWorkflow, NetworkIngressReconcileParams{IngressID: id})
	if err != nil {
		return nil, fmt.Errorf("start network ingress reconciliation: %w", err)
	}
	return run, nil
}

func (c *NetworkIngressClient) SignalNetworkIngress(ctx context.Context, id uuid.UUID) error {
	_, err := c.start(ctx, id)
	return err
}

func (c *NetworkIngressClient) RefreshNetworkIngress(ctx context.Context, id uuid.UUID) error {
	run, err := c.start(ctx, id)
	if err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = run.GetWithOptions(waitCtx, nil, client.WorkflowRunGetOptions{DisableFollowingRuns: true})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		return nil // The response keeps its last observed timestamp on timeout.
	}
	if _, ok := errors.AsType[*workflow.ContinueAsNewError](err); ok {
		return nil
	}
	return fmt.Errorf("wait for network ingress observation: %w", err)
}

func NetworkIngressReconcileWorkflow(ctx workflow.Context, params NetworkIngressReconcileParams) (NetworkIngressReconcileResult, error) {
	return Debounce(networkIngressReconcilePass, NetworkIngressReconcileWorkflow,
		func(NetworkIngressReconcileParams) string { return "reconcile" },
		func(_ NetworkIngressReconcileParams, result NetworkIngressReconcileResult) bool {
			return result.Requeue
		},
	)(ctx, params)
}

func networkIngressReconcilePass(ctx workflow.Context, params NetworkIngressReconcileParams) (NetworkIngressReconcileResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute, ScheduleToCloseTimeout: 5 * time.Minute,
		HeartbeatTimeout: 30 * time.Second,
		RetryPolicy:      &temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute, MaximumAttempts: 5},
	})
	var result NetworkIngressReconcileResult
	if err := workflow.ExecuteActivity(ctx, NetworkIngressReconcileActivityName, params.IngressID).Get(ctx, &result); err != nil {
		return result, fmt.Errorf("reconcile network ingress: %w", err)
	}
	if result.Requeue {
		if err := workflow.Sleep(ctx, time.Second); err != nil {
			return result, fmt.Errorf("delay changed network ingress: %w", err)
		}
	}
	return result, nil
}
