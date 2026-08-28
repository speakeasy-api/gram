package background

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	OpenRouterAdminCaptureCursorActivityName = "OpenRouterAdminCaptureCursor"
	OpenRouterAdminReconcileActivityName     = "OpenRouterAdminReconcile"
	OpenRouterAdminPermanentErrorType        = "OpenRouterAdminPermanentLocalError"
	OpenRouterAdminBeginUpdate               = "begin"
	OpenRouterAdminCompleteUpdate            = "complete"
	OpenRouterAdminAbortSignal               = "abort"

	openRouterAdminStartTimeout = 10 * time.Second
	// Strictly exceeds openrouterkeys' six-second local mutation ceiling.
	openRouterAdminGuardDelay = 10 * time.Second
)

func OpenRouterAdminReconciliationWorkflowID(scope openrouterkeys.AdminReconciliationScope) string {
	return "v1:openrouter-admin-reconcile:" + scope.OrganizationID + ":" + scope.KeyType
}

func OpenRouterAdminReconciliationWorkflow(ctx workflow.Context, scope openrouterkeys.AdminReconciliationScope) error {
	return openRouterAdminReconciliationWorkflow(ctx, scope, openRouterAdminGuardDelay)
}

func openRouterAdminReconciliationWorkflow(ctx workflow.Context, scope openrouterkeys.AdminReconciliationScope, guardDelay time.Duration) error {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, MaximumInterval: time.Minute, BackoffCoefficient: 2},
	}
	abort := workflow.GetSignalChannel(ctx, OpenRouterAdminAbortSignal)
	wake := workflow.NewBufferedChannel(ctx, 100)
	var cursor int64
	var hasCursor bool
	var generation int64
	armed := false

	capture := func(callCtx workflow.Context) (int64, error) {
		var captured int64
		callActivityCtx := workflow.WithActivityOptions(callCtx, activityOptions)
		if err := workflow.ExecuteActivity(callActivityCtx, OpenRouterAdminCaptureCursorActivityName, scope).Get(callCtx, &captured); err != nil {
			return 0, err
		}
		return captured, nil
	}
	reconcile := func(callCtx workflow.Context, baseline int64) (int64, error) {
		checkpoint := openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: baseline}
		var advanced int64
		callActivityCtx := workflow.WithActivityOptions(callCtx, activityOptions)
		if err := workflow.ExecuteActivity(callActivityCtx, OpenRouterAdminReconcileActivityName, checkpoint).Get(callCtx, &advanced); err != nil {
			return 0, err
		}
		return advanced, nil
	}

	if err := workflow.SetUpdateHandler(ctx, OpenRouterAdminBeginUpdate, func(handlerCtx workflow.Context) error {
		captured, err := capture(handlerCtx)
		if err != nil {
			return err
		}
		if !hasCursor || captured < cursor {
			cursor, hasCursor = captured, true
		}
		armed = true
		generation++
		wake.Send(handlerCtx, nil)
		return nil
	}); err != nil {
		return fmt.Errorf("register OpenRouter admin Begin update: %w", err)
	}
	if err := workflow.SetUpdateHandler(ctx, OpenRouterAdminCompleteUpdate, func(handlerCtx workflow.Context) error {
		if !hasCursor {
			// A Complete update can win Update-With-Start after the predecessor's
			// guard already reconciled and closed. Without that predecessor's Begin
			// baseline there is no bounded commit proof left to inspect. Keep the
			// successor alive for one guard window so concurrent Complete updates
			// are acknowledged without starting a chain of idle runs.
			armed = true
			generation++
			wake.Send(handlerCtx, nil)
			return nil
		}
		armed = true
		generation++
		wake.Send(handlerCtx, nil)

		advanced, err := reconcile(handlerCtx, cursor)
		if err != nil {
			return err
		}
		if advanced > cursor {
			cursor = advanced
		}
		return nil
	}); err != nil {
		return fmt.Errorf("register OpenRouter admin Complete update: %w", err)
	}

	for {
		if !armed {
			selector := workflow.NewSelector(ctx)
			selector.AddReceive(wake, func(channel workflow.ReceiveChannel, _ bool) { channel.Receive(ctx, nil) })
			selector.AddReceive(abort, func(channel workflow.ReceiveChannel, _ bool) { channel.Receive(ctx, nil) })
			selector.Select(ctx)
			if !armed {
				if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
					return fmt.Errorf("await idle OpenRouter admin handlers: %w", err)
				}
				if !armed {
					return nil
				}
			}
		}

		guardGeneration := generation
		timerCtx, cancelTimer := workflow.WithCancel(ctx)
		guard := workflow.NewTimer(timerCtx, guardDelay)
		woken := false
		selector := workflow.NewSelector(ctx)
		selector.AddFuture(guard, func(workflow.Future) {})
		selector.AddReceive(wake, func(channel workflow.ReceiveChannel, _ bool) {
			channel.Receive(ctx, nil)
			woken = true
		})
		selector.AddReceive(abort, func(channel workflow.ReceiveChannel, _ bool) {
			channel.Receive(ctx, nil)
			woken = true
		})
		selector.Select(ctx)
		if woken {
			cancelTimer()
			continue
		}

		if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
			return fmt.Errorf("await guarded OpenRouter admin handlers: %w", err)
		}
		if generation != guardGeneration {
			continue
		}
		if !hasCursor {
			return nil
		}
		advanced, err := reconcile(ctx, cursor)
		if err != nil {
			return err
		}
		if !hasCursor || advanced > cursor {
			cursor, hasCursor = advanced, true
		}
		if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
			return fmt.Errorf("await reconciled OpenRouter admin handlers: %w", err)
		}
		if generation != guardGeneration {
			continue
		}
		return nil
	}
}

type adminStateReconciler interface {
	CaptureCursor(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error)
	ReconcileSince(context.Context, openrouterkeys.AdminReconciliationCheckpoint) (int64, error)
}

type OpenRouterAdminReconciliationActivities struct {
	logger     *slog.Logger
	reconciler adminStateReconciler
}

func NewOpenRouterAdminReconciliationActivities(logger *slog.Logger, reconciler adminStateReconciler) *OpenRouterAdminReconciliationActivities {
	return &OpenRouterAdminReconciliationActivities{logger: logger, reconciler: reconciler}
}

func (a *OpenRouterAdminReconciliationActivities) CaptureCursor(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) (int64, error) {
	cursor, err := a.reconciler.CaptureCursor(ctx, scope)
	if err != nil {
		//nolint:wrapcheck // Temporal's application error must remain outermost for retry classification.
		return 0, temporal.NewApplicationError("transient OpenRouter admin cursor capture failure", "OpenRouterAdminTransientError")
	}
	return cursor, nil
}

func (a *OpenRouterAdminReconciliationActivities) Reconcile(ctx context.Context, checkpoint openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
	cursor, err := a.reconciler.ReconcileSince(ctx, checkpoint)
	if err == nil {
		return cursor, nil
	}
	scope := checkpoint.Scope
	a.logger.ErrorContext(ctx, "OpenRouter admin reconciliation failed", attr.SlogError(err), attr.SlogOrganizationID(scope.OrganizationID), attr.SlogOpenRouterKeyType(scope.KeyType))
	if openrouterkeys.IsPermanentAdminReconciliationError(err) {
		return 0, temporal.NewNonRetryableApplicationError("permanent local OpenRouter admin reconciliation error", OpenRouterAdminPermanentErrorType, nil)
	}
	//nolint:wrapcheck // Temporal's application error must remain outermost for retry classification.
	return 0, temporal.NewApplicationError("transient OpenRouter admin reconciliation failure", "OpenRouterAdminTransientError")
}

type TemporalOpenRouterAdminCoordinator struct{ TemporalEnv *tenv.Environment }

var _ openrouterkeys.AdminMutationCoordinator = (*TemporalOpenRouterAdminCoordinator)(nil)

func (c *TemporalOpenRouterAdminCoordinator) Begin(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) error {
	return c.updateWithStart(ctx, scope, OpenRouterAdminBeginUpdate)
}

func (c *TemporalOpenRouterAdminCoordinator) CompleteAndWait(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) error {
	return c.updateWithStart(ctx, scope, OpenRouterAdminCompleteUpdate)
}

func (c *TemporalOpenRouterAdminCoordinator) updateWithStart(ctx context.Context, scope openrouterkeys.AdminReconciliationScope, updateName string) error {
	workflowID := OpenRouterAdminReconciliationWorkflowID(scope)
	startCtx, cancel := context.WithTimeout(ctx, openRouterAdminStartTimeout)
	defer cancel()
	start := c.TemporalEnv.Client().NewWithStartWorkflowOperation(client.StartWorkflowOptions{
		ID: workflowID, TaskQueue: string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}, OpenRouterAdminReconciliationWorkflow, scope)
	handle, err := c.TemporalEnv.Client().UpdateWithStartWorkflow(startCtx, client.UpdateWithStartWorkflowOptions{
		StartWorkflowOperation: start,
		UpdateOptions: client.UpdateWorkflowOptions{
			UpdateName:   updateName,
			WaitForStage: client.WorkflowUpdateStageAccepted,
		},
	})
	if err != nil {
		return fmt.Errorf("accept OpenRouter admin %s update: %w", updateName, err)
	}
	if err := handle.Get(ctx, nil); err != nil {
		return fmt.Errorf("complete OpenRouter admin %s update: %w", updateName, err)
	}
	return nil
}

func (c *TemporalOpenRouterAdminCoordinator) Abort(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) error {
	workflowID := OpenRouterAdminReconciliationWorkflowID(scope)
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openRouterAdminStartTimeout)
	defer cancel()
	_, err := c.TemporalEnv.Client().SignalWithStartWorkflow(startCtx, workflowID, OpenRouterAdminAbortSignal, nil, client.StartWorkflowOptions{
		ID: workflowID, TaskQueue: string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}, OpenRouterAdminReconciliationWorkflow, scope)
	if err != nil {
		return fmt.Errorf("signal OpenRouter admin reconciliation abort: %w", err)
	}
	return nil
}
