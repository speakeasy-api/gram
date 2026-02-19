package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type FallbackModelUsageTrackingParams struct {
	GenerationID string
	OrgID        string
	ProjectID    string
	Source       billing.ModelUsageSource
	ChatID       string
}

// FallbackModelUsageTracker implements chat.FallbackModelUsageTracker using Temporal.
type FallbackModelUsageTracker struct {
	TemporalEnv *tenv.Environment
}

func (t *FallbackModelUsageTracker) ScheduleFallbackModelUsageTracking(ctx context.Context, generationID, orgID, projectID string, source billing.ModelUsageSource, chatID string) error {
	_, err := ExecuteFallbackModelUsageTrackingWorkflow(ctx, t.TemporalEnv, FallbackModelUsageTrackingParams{
		GenerationID: generationID,
		OrgID:        orgID,
		ProjectID:    projectID,
		Source:       source,
		ChatID:       chatID,
	})
	return err
}

func ExecuteFallbackModelUsageTrackingWorkflow(ctx context.Context, env *tenv.Environment, params FallbackModelUsageTrackingParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:fallback-model-usage-tracking:%s", params.GenerationID)
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:    10 * time.Minute,
		StartDelay:            time.Minute, // Delay initial run by 1 minute to allow OpenRouter generation data to become available
	}, FallbackModelUsageTrackingWorkflow, params)
}

func FallbackModelUsageTrackingWorkflow(ctx workflow.Context, params FallbackModelUsageTrackingParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    time.Minute,
			BackoffCoefficient: 1.0, // No exponential backoff, keep 1 minute between retries
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities
	err := workflow.ExecuteActivity(
		ctx,
		a.FallbackModelUsageTracking,
		activities.FallbackModelUsageTrackingArgs{
			GenerationID: params.GenerationID,
			OrgID:        params.OrgID,
			ProjectID:    params.ProjectID,
			Source:       params.Source,
			ChatID:       params.ChatID,
		},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to track model usage in workflow: %w", err)
	}
	return nil
}
