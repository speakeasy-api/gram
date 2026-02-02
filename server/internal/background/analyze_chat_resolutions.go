package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type AnalyzeChatResolutionsParams struct {
	ChatID    uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
}

// ChatResolutionAnalyzer schedules async chat resolution analysis.
type ChatResolutionAnalyzer interface {
	ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID string) error
}

// TemporalChatResolutionAnalyzer implements ChatResolutionAnalyzer using Temporal.
type TemporalChatResolutionAnalyzer struct {
	Temporal client.Client
}

func (t *TemporalChatResolutionAnalyzer) ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID string) error {
	_, err := ExecuteAnalyzeChatResolutionsWorkflow(ctx, t.Temporal, AnalyzeChatResolutionsParams{
		ChatID:    chatID,
		ProjectID: projectID,
		OrgID:     orgID,
	})
	return err
}

func ExecuteAnalyzeChatResolutionsWorkflow(ctx context.Context, temporalClient client.Client, params AnalyzeChatResolutionsParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:analyze-chat-resolutions:%s", params.ChatID.String())
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:    3 * time.Minute,
	}, AnalyzeChatResolutionsWorkflow, params)
}

func AnalyzeChatResolutionsWorkflow(ctx workflow.Context, params AnalyzeChatResolutionsParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 90 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	})

	var a *Activities

	// Phase 1: Analyze tool call outcomes
	err := workflow.ExecuteActivity(
		ctx,
		a.AnalyzeToolCallOutcomes,
		activities.AnalyzeToolCallOutcomesArgs{
			ChatID:    params.ChatID,
			ProjectID: params.ProjectID,
			OrgID:     params.OrgID,
		},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to analyze tool call outcomes: %w", err)
	}

	// Phase 2: Analyze overall chat resolutions
	err = workflow.ExecuteActivity(
		ctx,
		a.AnalyzeChatResolutions,
		activities.AnalyzeChatResolutionsArgs{
			ChatID:    params.ChatID,
			ProjectID: params.ProjectID,
			OrgID:     params.OrgID,
		},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to analyze chat resolutions: %w", err)
	}

	return nil
}
