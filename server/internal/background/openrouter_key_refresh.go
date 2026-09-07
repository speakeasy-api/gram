package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/constants"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type OpenRouterKeyRefreshParams struct {
	OrgID string
	Limit *int // Allows for setting custom limits by kicking off this temporal workflow directly
	// KeyType names which of the org's OpenRouter keys to refresh ("chat" or
	// "internal"). Empty resolves to the chat key, keeping in-flight payloads
	// from before the field existed valid.
	KeyType string
}

type OpenRouterKeyRefresher struct {
	TemporalEnv *tenv.Environment
}

// SetOpenRouterSpendCap starts one durable cap operation and waits until its
// upstream PATCH, local mirror update, and audit entry have all completed.
func (w *OpenRouterKeyRefresher) SetOpenRouterSpendCap(ctx context.Context, operationID, orgID string, keyType openrouter.KeyType, limit int, actor urn.Principal, actorDisplayName *string) error {
	_, err := w.setOpenRouterSpendCap(ctx, operationID, orgID, keyType, limit, actor, actorDisplayName, false)
	return err
}

// SetAdminOpenRouterSpendCap bypasses customer billing policy while retaining
// the durable key update and audit operation.
func (w *OpenRouterKeyRefresher) SetAdminOpenRouterSpendCap(ctx context.Context, operationID, orgID string, keyType openrouter.KeyType, limit int, actor urn.Principal, actorDisplayName *string) (int, error) {
	return w.setOpenRouterSpendCap(ctx, operationID, orgID, keyType, limit, actor, actorDisplayName, true)
}

func (w *OpenRouterKeyRefresher) setOpenRouterSpendCap(ctx context.Context, operationID, orgID string, keyType openrouter.KeyType, limit int, actor urn.Principal, actorDisplayName *string, bypassPolicy bool) (int, error) {
	if operationID == "" {
		return 0, errors.New("spend-cap operation ID is required")
	}
	if orgID == "" {
		return 0, errors.New("organization ID is required")
	}
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return 0, fmt.Errorf("invalid OpenRouter key type: %w", err)
	}
	if limit < constants.MinimumPaygSpendCapUSD || limit > constants.MaximumPaygSpendCapUSD {
		return 0, fmt.Errorf("spend cap must be between %d and %d: %d", constants.MinimumPaygSpendCapUSD, constants.MaximumPaygSpendCapUSD, limit)
	}

	workflowID := fmt.Sprintf("v1:openrouter-spend-cap:%s:%s", keyType, operationID)
	workflowFunc := any(OpenRouterSpendCapWorkflow)
	if bypassPolicy {
		workflowFunc = AdminOpenRouterSpendCapWorkflow
	}
	run, err := w.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             string(w.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:    10 * time.Minute,
	}, workflowFunc, OpenRouterSpendCapParams{
		OperationID:      operationID,
		OrganizationID:   orgID,
		KeyType:          string(keyType),
		Limit:            limit,
		Actor:            actor,
		ActorDisplayName: actorDisplayName,
		BypassPolicy:     bypassPolicy,
	})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		run = w.TemporalEnv.Client().GetWorkflow(ctx, workflowID, "")
	case err != nil:
		return 0, fmt.Errorf("start OpenRouter spend-cap workflow: %w", err)
	}

	if !bypassPolicy {
		if err := run.Get(ctx, nil); err != nil {
			return 0, fmt.Errorf("complete OpenRouter spend-cap workflow: %w", err)
		}
		return 0, nil
	}
	var monthlyCredits int
	if err := run.Get(ctx, &monthlyCredits); err != nil {
		return 0, fmt.Errorf("complete OpenRouter spend-cap workflow: %w", err)
	}
	return monthlyCredits, nil
}

type OpenRouterSpendCapParams struct {
	OperationID    string
	OrganizationID string
	// KeyType is empty only for workflows created before per-key caps. Those
	// legacy payloads continue to target the other-inference (chat) key.
	KeyType          string
	Limit            int
	Actor            urn.Principal
	ActorDisplayName *string
	// BypassPolicy is false for existing and customer-initiated workflows.
	BypassPolicy bool
}

func OpenRouterSpendCapWorkflow(ctx workflow.Context, params OpenRouterSpendCapParams) error {
	params.BypassPolicy = false
	_, err := executeOpenRouterSpendCapWorkflow(ctx, params, false)
	return err
}

func AdminOpenRouterSpendCapWorkflow(ctx workflow.Context, params OpenRouterSpendCapParams) (int, error) {
	params.BypassPolicy = true
	return executeOpenRouterSpendCapWorkflow(ctx, params, true)
}

func executeOpenRouterSpendCapWorkflow(ctx workflow.Context, params OpenRouterSpendCapParams, returnResult bool) (int, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 45 * time.Second,
		// The heartbeat carries retry state; it does not extend StartToClose.
		// Lock acquisition is bounded at 10 seconds and the upstream PATCH at
		// 20 seconds, leaving time for the local mirror and audit transaction.
		HeartbeatTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
		},
	})

	var a *Activities
	future := workflow.ExecuteActivity(ctx, a.SetOpenRouterSpendCap, activities.SetOpenRouterSpendCapArgs{
		OperationID:      params.OperationID,
		OrganizationID:   params.OrganizationID,
		KeyType:          params.KeyType,
		Limit:            params.Limit,
		Actor:            params.Actor,
		ActorDisplayName: params.ActorDisplayName,
		BypassPolicy:     params.BypassPolicy,
	})
	if !returnResult {
		if err := future.Get(ctx, nil); err != nil {
			return 0, fmt.Errorf("set OpenRouter spend cap: %w", err)
		}
		return 0, nil
	}
	var monthlyCredits int
	if err := future.Get(ctx, &monthlyCredits); err != nil {
		return 0, fmt.Errorf("set OpenRouter spend cap: %w", err)
	}
	return monthlyCredits, nil
}

func (w *OpenRouterKeyRefresher) ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) error {
	_, err := ExecuteOpenrouterKeyRefreshWorkflow(ctx, w.TemporalEnv, OpenRouterKeyRefreshParams{
		OrgID:   orgID,
		Limit:   limit,
		KeyType: string(keyType),
	})
	return err
}

// SchedulePaygOpenRouterChatKeyReconciliation uses the durable outbox event ID
// as the workflow identity, so Pub/Sub redelivery cannot start the same
// reconciliation twice. The workflow reads current billing state instead of
// trusting the event's historical transition.
func (w *OpenRouterKeyRefresher) SchedulePaygOpenRouterChatKeyReconciliation(ctx context.Context, eventID, orgID string, desiredState openrouter.KeyDesiredState) error {
	if eventID == "" {
		return errors.New("PAYG billing event ID is required")
	}
	if orgID == "" {
		return errors.New("organization ID is required")
	}
	if err := desiredState.Validate(); err != nil {
		return fmt.Errorf("schedule PAYG OpenRouter chat key reconciliation: %w", err)
	}

	_, err := w.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    fmt.Sprintf("v1:openrouter-chat-key-reconcile:billing:%s", eventID),
		TaskQueue:             string(w.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:    3 * time.Minute,
	}, PaygOpenRouterChatKeyReconcileWorkflow, ReconcilePaygOpenRouterChatKeyParams{
		OrganizationID: orgID,
		DesiredState:   desiredState,
	})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		return nil
	case err != nil:
		return fmt.Errorf("start PAYG OpenRouter chat key reconciliation workflow: %w", err)
	default:
		return nil
	}
}

type ReconcilePaygOpenRouterChatKeyParams struct {
	OrganizationID string
	DesiredState   openrouter.KeyDesiredState
}

func PaygOpenRouterChatKeyReconcileWorkflow(ctx workflow.Context, params ReconcilePaygOpenRouterChatKeyParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(
		ctx,
		a.ReconcilePaygOpenRouterChatKey,
		activities.ReconcilePaygOpenRouterChatKeyArgs{OrganizationID: params.OrganizationID, DesiredState: params.DesiredState},
	).Get(ctx, nil); err != nil {
		return fmt.Errorf("reconcile PAYG OpenRouter chat key: %w", err)
	}

	return nil
}

type EnterpriseTrialConversionKeyReconcileParams struct {
	OrganizationID string
}

func (w *OpenRouterKeyRefresher) ScheduleEnterpriseTrialConversionKeyReconciliation(ctx context.Context, eventID, orgID string) error {
	if eventID == "" {
		return errors.New("outbox event ID is required")
	}
	if orgID == "" {
		return errors.New("organization ID is required")
	}
	_, err := w.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    fmt.Sprintf("v1:openrouter-key-reconcile:enterprise-trial-conversion:%s", eventID),
		TaskQueue:             string(w.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, EnterpriseTrialConversionKeyReconcileWorkflow, EnterpriseTrialConversionKeyReconcileParams{OrganizationID: orgID})
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		return nil
	case err != nil:
		return fmt.Errorf("start enterprise trial conversion key reconciliation workflow: %w", err)
	default:
		return nil
	}
}

func EnterpriseTrialConversionKeyReconcileWorkflow(ctx workflow.Context, params EnterpriseTrialConversionKeyReconcileParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Hour,
		},
	})
	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.ReconcileEnterpriseTrialConversionKeys, activities.ReconcileEnterpriseTrialConversionKeysArgs{OrganizationID: params.OrganizationID}).Get(ctx, nil); err != nil {
		return fmt.Errorf("reconcile enterprise trial conversion keys: %w", err)
	}
	return nil
}

// Called by your service to start (or restart) the workflow
func ExecuteOpenrouterKeyRefreshWorkflow(ctx context.Context, temporalEnv *tenv.Environment, params OpenRouterKeyRefreshParams) (client.WorkflowRun, error) {
	// A typoed key type must fail here, before the terminate-if-running id
	// below can clobber the real chat refresh workflow.
	if err := openrouter.KeyType(params.KeyType).Validate(); err != nil {
		return nil, fmt.Errorf("refresh openrouter key workflow: %w", err)
	}
	// The chat key keeps the historical id format: cancel semantics and the
	// manual-trigger docs reference it. Only internal keys get a suffix.
	id := fmt.Sprintf("v1:openrouter-key-refresh:%s", params.OrgID)
	if openrouter.KeyType(params.KeyType) == openrouter.KeyTypeInternal {
		id += ":internal"
	}
	return temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(temporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_TERMINATE_IF_RUNNING,
		WorkflowRunTimeout:    3 * time.Minute, // slightly longer workflow timeout
	}, OpenrouterKeyRefreshWorkflow, params)
}

func OpenrouterKeyRefreshWorkflow(ctx workflow.Context, params OpenRouterKeyRefreshParams) error {
	logger := workflow.GetLogger(ctx)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	var a *Activities
	err := workflow.ExecuteActivity(
		ctx,
		a.RefreshOpenRouterKey,
		activities.RefreshOpenRouterKeyArgs{OrgID: params.OrgID, Limit: params.Limit, KeyType: params.KeyType},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to refresh openrouter key: %w", err)
	}

	logger.Info("Key refresh succeeded; continuing workflow for next cycle", "OrgID", params.OrgID)

	// kick off a new workflow loop with clean history
	return nil
}
