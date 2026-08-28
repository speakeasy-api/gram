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
	reconcileActivityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: time.Second, MaximumInterval: time.Minute, BackoffCoefficient: 2},
	}
	captureActivityOptions := reconcileActivityOptions
	captureActivityOptions.RetryPolicy = &temporal.RetryPolicy{
		InitialInterval: time.Second, MaximumInterval: time.Minute, BackoffCoefficient: 2, MaximumAttempts: 3,
	}
	abort := workflow.GetSignalChannel(ctx, OpenRouterAdminAbortSignal)
	wake := workflow.NewBufferedChannel(ctx, 100)
	operations := map[int64]int64{}
	var generation int64
	var lastToken int64
	var pendingClose bool

	capture := func(callCtx workflow.Context) (int64, error) {
		var captured int64
		callActivityCtx := workflow.WithActivityOptions(callCtx, captureActivityOptions)
		if err := workflow.ExecuteActivity(callActivityCtx, OpenRouterAdminCaptureCursorActivityName, scope).Get(callCtx, &captured); err != nil {
			return 0, err
		}
		return captured, nil
	}
	reconcile := func(callCtx workflow.Context, baseline int64) (int64, error) {
		checkpoint := openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: baseline}
		var advanced int64
		callActivityCtx := workflow.WithActivityOptions(callCtx, reconcileActivityOptions)
		if err := workflow.ExecuteActivity(callActivityCtx, OpenRouterAdminReconcileActivityName, checkpoint).Get(callCtx, &advanced); err != nil {
			return 0, err
		}
		return advanced, nil
	}

	if err := workflow.SetUpdateHandler(ctx, OpenRouterAdminBeginUpdate, func(handlerCtx workflow.Context) (int64, error) {
		captured, err := capture(handlerCtx)
		if err != nil {
			wake.Send(handlerCtx, nil)
			return 0, err
		}
		token := workflow.Now(handlerCtx).UnixNano()
		if token <= lastToken {
			token = lastToken + 1
		}
		lastToken = token
		operations[token] = captured
		pendingClose = false
		generation++
		wake.Send(handlerCtx, nil)
		return token, nil
	}); err != nil {
		return fmt.Errorf("register OpenRouter admin Begin update: %w", err)
	}
	if err := workflow.SetUpdateHandler(ctx, OpenRouterAdminCompleteUpdate, func(handlerCtx workflow.Context, token int64) error {
		baseline, ok := operations[token]
		if !ok {
			// A retry can reach a successor after the operation's workflow closed.
			// Without its Begin baseline there is no bounded commit proof to inspect.
			pendingClose = true
			generation++
			wake.Send(handlerCtx, nil)
			return nil
		}
		generation++
		wake.Send(handlerCtx, nil)

		if _, err := reconcile(handlerCtx, baseline); err != nil {
			return err
		}
		delete(operations, token)
		pendingClose = false
		generation++
		wake.Send(handlerCtx, nil)
		return nil
	}); err != nil {
		return fmt.Errorf("register OpenRouter admin Complete update: %w", err)
	}

	receiveAbort := func(channel workflow.ReceiveChannel) {
		var token int64
		channel.Receive(ctx, &token)
		if _, ok := operations[token]; ok {
			delete(operations, token)
			generation++
		}
	}
	oldestCursor := func() (int64, bool) {
		var oldest int64
		found := false
		for _, cursor := range operations {
			if !found || cursor < oldest {
				oldest, found = cursor, true
			}
		}
		return oldest, found
	}

	for {
		if len(operations) == 0 && !pendingClose {
			selector := workflow.NewSelector(ctx)
			selector.AddReceive(wake, func(channel workflow.ReceiveChannel, _ bool) { channel.Receive(ctx, nil) })
			selector.AddReceive(abort, func(channel workflow.ReceiveChannel, _ bool) { receiveAbort(channel) })
			selector.Select(ctx)
			if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
				return fmt.Errorf("await idle OpenRouter admin handlers: %w", err)
			}
			if len(operations) == 0 && !pendingClose {
				return nil
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
			receiveAbort(channel)
			woken = true
		})
		selector.Select(ctx)
		if woken {
			cancelTimer()
			if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
				return fmt.Errorf("await updated OpenRouter admin handlers: %w", err)
			}
			if len(operations) == 0 && !pendingClose {
				return nil
			}
			continue
		}

		if err := workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) }); err != nil {
			return fmt.Errorf("await guarded OpenRouter admin handlers: %w", err)
		}
		if generation != guardGeneration {
			continue
		}
		baseline, ok := oldestCursor()
		if !ok {
			return nil
		}
		if _, err := reconcile(ctx, baseline); err != nil {
			return err
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

func (c *TemporalOpenRouterAdminCoordinator) Begin(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) (int64, error) {
	var token int64
	if err := c.updateWithStart(ctx, scope, OpenRouterAdminBeginUpdate, &token); err != nil {
		return 0, err
	}
	return token, nil
}

func (c *TemporalOpenRouterAdminCoordinator) CompleteAndWait(ctx context.Context, scope openrouterkeys.AdminReconciliationScope, token int64) error {
	return c.updateWithStart(ctx, scope, OpenRouterAdminCompleteUpdate, nil, token)
}

func (c *TemporalOpenRouterAdminCoordinator) updateWithStart(ctx context.Context, scope openrouterkeys.AdminReconciliationScope, updateName string, result any, args ...any) error {
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
			Args:         args,
		},
	})
	if err != nil {
		return fmt.Errorf("accept OpenRouter admin %s update: %w", updateName, err)
	}
	if err := handle.Get(ctx, result); err != nil {
		return fmt.Errorf("complete OpenRouter admin %s update: %w", updateName, err)
	}
	return nil
}

func (c *TemporalOpenRouterAdminCoordinator) Abort(ctx context.Context, scope openrouterkeys.AdminReconciliationScope, token int64) error {
	workflowID := OpenRouterAdminReconciliationWorkflowID(scope)
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openRouterAdminStartTimeout)
	defer cancel()
	_, err := c.TemporalEnv.Client().SignalWithStartWorkflow(startCtx, workflowID, OpenRouterAdminAbortSignal, token, client.StartWorkflowOptions{
		ID: workflowID, TaskQueue: string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}, OpenRouterAdminReconciliationWorkflow, scope)
	if err != nil {
		return fmt.Errorf("signal OpenRouter admin reconciliation abort: %w", err)
	}
	return nil
}
