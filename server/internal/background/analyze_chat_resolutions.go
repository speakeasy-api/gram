package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	activities "github.com/speakeasy-api/gram/server/internal/background/activities/chat_resolutions"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
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

func ScheduleChatResolutionAnalysis(ctx context.Context, env *tenv.Environment, chatID, projectID uuid.UUID, orgID, apiKeyID string) error {
	_, err := ExecuteAnalyzeChatResolutionsWorkflow(ctx, env, AnalyzeChatResolutionsParams{
		ChatID:    chatID,
		ProjectID: projectID,
		OrgID:     orgID,
		APIKeyID:  apiKeyID,
	})
	return err
}

func ExecuteAnalyzeChatResolutionsWorkflow(ctx context.Context, env *tenv.Environment, params AnalyzeChatResolutionsParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:analyze-chat-resolutions:%s", params.ChatID.String())
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
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

	// Phase 0: Pin the chat generation and load user feedback against it. The
	// generation must be reused by every subsequent activity, otherwise a
	// generation bump mid-workflow can replace the message set and silently
	// invalidate the indices computed here.
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
		return fmt.Errorf("get user feedback for chat: %w", err)
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
			Generation:   feedbackResult.Generation,
			UserFeedback: feedbackResult.UserFeedback,
		},
	).Get(ctx, &segmentOutput)
	if err != nil {
		if activities.IsGenerationBumped(err) {
			workflow.GetLogger(ctx).Info("chat generation bumped during segmentation, restarting analysis on latest generation",
				"chat_id", params.ChatID.String(),
			)
			return workflow.NewContinueAsNewError(ctx, AnalyzeChatResolutionsWorkflow, params)
		}
		return fmt.Errorf("segment chat: %w", err)
	}

	err = workflow.ExecuteActivity(
		ctx,
		a.DeleteChatResolutions,
		activities.DeleteChatResolutionsArgs{
			ChatID: params.ChatID,
		},
	).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete existing resolutions: %w", err)
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
				APIKeyID:     params.APIKeyID,
				Generation:   feedbackResult.Generation,
				StartIndex:   segment.StartIndex,
				EndIndex:     segment.EndIndex,
				UserFeedback: feedbackResult.UserFeedback,
			},
		).Get(ctx, nil)
		if err != nil {
			if activities.IsGenerationBumped(err) {
				workflow.GetLogger(ctx).Info("chat generation bumped during segment analysis, restarting on latest generation",
					"chat_id", params.ChatID.String(),
					"start_index", segment.StartIndex,
					"end_index", segment.EndIndex,
				)
				return workflow.NewContinueAsNewError(ctx, AnalyzeChatResolutionsWorkflow, params)
			}
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
