package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	activities "github.com/speakeasy-api/gram/server/internal/background/activities/chat_resolutions"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type AnalyzeChatResolutionsParams struct {
	ChatID    uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
	APIKeyID  string
}

// ChatResolutionAnalyzer schedules async chat resolution analysis.
type ChatResolutionAnalyzer interface {
	ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID, apiKeyID string) error
}

// TemporalChatResolutionAnalyzer implements ChatResolutionAnalyzer using Temporal.
type TemporalChatResolutionAnalyzer struct {
	Temporal client.Client
}

func (t *TemporalChatResolutionAnalyzer) ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID, apiKeyID string) error {
	_, err := ExecuteAnalyzeChatResolutionsWorkflow(ctx, t.Temporal, AnalyzeChatResolutionsParams{
		ChatID:    chatID,
		ProjectID: projectID,
		OrgID:     orgID,
		APIKeyID:  apiKeyID,
	})
	return err
}

func ExecuteAnalyzeChatResolutionsWorkflow(ctx context.Context, temporalClient client.Client, params AnalyzeChatResolutionsParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:analyze-chat-resolutions:%s", params.ChatID.String())
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE, // Necessary for chats that are resumed after a while
		WorkflowRunTimeout:    5 * time.Minute,
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

	// Phase 0: Get user feedback message ID if it exists
	var feedbackResult activities.GetUserFeedbackForChatResult
	err := workflow.ExecuteActivity(
		ctx,
		a.GetUserFeedbackForChat,
		activities.GetUserFeedbackForChatArgs{
			ProjectID: params.ProjectID,
			ChatID:    params.ChatID,
		},
	).Get(ctx, &feedbackResult)
	if err != nil {
		return fmt.Errorf("failed to get user feedback message ID: %w", err)
	}

	// Phase 1: Segment the chat into logical breakpoints
	// Pass feedback message IDs as hints for segmentation
	var segmentOutput activities.SegmentChatOutput
	err = workflow.ExecuteActivity(
		ctx,
		a.SegmentChat,
		activities.SegmentChatArgs{
			ChatID:       params.ChatID,
			ProjectID:    params.ProjectID,
			OrgID:        params.OrgID,
			APIKeyID:     params.APIKeyID,
			UserFeedback: feedbackResult.UserFeedback,
		},
	).Get(ctx, &segmentOutput)
	if err != nil {
		return fmt.Errorf("failed to segment chat: %w", err)
	}

	err = workflow.ExecuteActivity(
		ctx,
		a.DeleteChatResolutions,
		activities.DeleteChatResolutionsArgs{
			ChatID: params.ChatID,
		},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to delete existing resolutions: %w", err)
	}

	// Phase 2: Analyze each segment comprehensively
	for _, segment := range segmentOutput.Segments {
		err := workflow.ExecuteActivity(
			ctx,
			a.AnalyzeSegment,
			activities.AnalyzeSegmentArgs{
				ChatID:       params.ChatID,
				ProjectID:    params.ProjectID,
				OrgID:        params.OrgID,
				StartIndex:   segment.StartIndex,
				EndIndex:     segment.EndIndex,
				APIKeyID:     params.APIKeyID,
				UserFeedback: feedbackResult.UserFeedback,
			},
		).Get(ctx, nil)
		if err != nil {
			// Log but continue with other segments
			workflow.GetLogger(ctx).Error("failed to analyze segment",
				"error", err.Error(),
				"start_index", segment.StartIndex,
				"end_index", segment.EndIndex,
			)
			continue
		}
	}

	return nil
}
