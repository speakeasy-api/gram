package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type OpenRouterKeyRefreshParams struct {
	OrgID string
	Limit *int // Allows for setting custom limits by kicking off this temporal workflow directly
}

type OpenRouterKeyRefresher struct {
	TemporalEnv *tenv.Environment
}

func (w *OpenRouterKeyRefresher) ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string) error {
	_, err := ExecuteOpenrouterKeyRefreshWorkflow(ctx, w.TemporalEnv, OpenRouterKeyRefreshParams{
		OrgID: orgID,
		Limit: nil,
	})
	return err
}

// CancelOpenRouterKeyRefreshWorkflow cancels an existing openrouter key refresh workflow for the given orgID
func (w *OpenRouterKeyRefresher) CancelOpenRouterKeyRefreshWorkflow(ctx context.Context, orgID string) error {
	if w.TemporalEnv == nil {
		return nil // No-op if Temporal client is not available
	}

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
	id := fmt.Sprintf("v1:openrouter-key-refresh:%s", params.OrgID)
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
	var toolsResponse activities.SlackProjectContextResponse
	err := workflow.ExecuteActivity(
		ctx,
		a.RefreshOpenRouterKey,
		activities.RefreshOpenRouterKeyArgs{OrgID: params.OrgID, Limit: params.Limit},
	).Get(ctx, &toolsResponse)
	if err != nil {
		return fmt.Errorf("failed to refresh openrouter key: %w", err)
	}

	logger.Info("Key refresh succeeded; continuing workflow for next cycle", "OrgID", params.OrgID)

	// kick off a new workflow loop with clean history
	return nil
}
