package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"go.temporal.io/api/enums/v1"
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

func (w *OpenRouterKeyRefresher) ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string, keyType openrouter.KeyType) error {
	_, err := ExecuteOpenrouterKeyRefreshWorkflow(ctx, w.TemporalEnv, OpenRouterKeyRefreshParams{
		OrgID:   orgID,
		Limit:   nil,
		KeyType: string(keyType),
	})
	return err
}

// CancelOpenRouterKeyRefreshWorkflow cancels an existing openrouter key refresh workflow for the given orgID
func (w *OpenRouterKeyRefresher) CancelOpenRouterKeyRefreshWorkflow(ctx context.Context, orgID string) error {
	id := fmt.Sprintf("v1:openrouter-key-refresh:%s", orgID)
	err := w.TemporalEnv.Client().CancelWorkflow(ctx, id, "")
	if err != nil {
		// If workflow doesn't exist, that's fine - it may have already completed or never started
		return nil
	}

	return nil
}

// Called by your service to start (or restart) the workflow
func ExecuteOpenrouterKeyRefreshWorkflow(ctx context.Context, temporalEnv *tenv.Environment, params OpenRouterKeyRefreshParams) (client.WorkflowRun, error) {
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
