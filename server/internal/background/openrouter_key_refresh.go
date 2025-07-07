package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// TODO: This will be 30 just setting to a lower value initially to test
const OpenRouterKeyRefreshWindow = 30

type OpenRouterKeyRefreshParams struct {
	OrgID string
}

type OpenRouterKeyRefresher struct {
	Temporal client.Client
}

func (w *OpenRouterKeyRefresher) ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string) error {
	_, err := ExecuteOpenrouterKeyRefreshWorkflow(ctx, w.Temporal, OpenRouterKeyRefreshParams{
		OrgID: orgID,
	})
	return err
}

// Called by your service to start (or restart) the workflow
func ExecuteOpenrouterKeyRefreshWorkflow(ctx context.Context, temporalClient client.Client, params OpenRouterKeyRefreshParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:openrouter-key-refresh:%s", params.OrgID)
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:    (OpenRouterKeyRefreshWindow + 1) * 24 * time.Hour, // slightly longer workflow timeout
	}, OpenrouterKeyRefreshWorkflow, params)
}

func OpenrouterKeyRefreshWorkflow(ctx workflow.Context, params OpenRouterKeyRefreshParams) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Sleeping for 30 days before key refresh", "OrgID", params.OrgID)

	if err := workflow.Sleep(ctx, OpenRouterKeyRefreshWindow*24*time.Hour); err != nil {
		return fmt.Errorf("workflow sleep interrupted: %w", err)
	}

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
		activities.RefreshOpenRouterKeyArgs{OrgID: params.OrgID},
	).Get(ctx, &toolsResponse)
	if err != nil {
		return fmt.Errorf("failed to refresh openrouter key: %w", err)
	}

	logger.Info("Key refresh succeeded; continuing workflow for next cycle", "OrgID", params.OrgID)

	// kick off a new workflow loop with clean history
	return workflow.NewContinueAsNewError(ctx, OpenrouterKeyRefreshWorkflow, params)
}
