package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
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
