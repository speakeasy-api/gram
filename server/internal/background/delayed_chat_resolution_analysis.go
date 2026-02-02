package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

const (
	ChatResolutionInactivityDuration = 30 * time.Minute
	SignalResetTimer                 = "reset-timer"
)

type DelayedChatResolutionAnalysisParams struct {
	ChatID    uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
}

// TemporalDelayedChatResolutionAnalyzer schedules delayed chat resolution analysis with inactivity detection.
type TemporalDelayedChatResolutionAnalyzer struct {
	Temporal client.Client
}

func (t *TemporalDelayedChatResolutionAnalyzer) ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID string) error {
	workflowID := fmt.Sprintf("v1:delayed-chat-resolution-analysis:%s", chatID.String())

	// First, try to signal an existing workflow to reset the timer
	err := t.Temporal.SignalWorkflow(ctx, workflowID, "", SignalResetTimer, nil)
	if err == nil {
		// Successfully reset the timer on existing workflow
		return nil
	}

	// Workflow doesn't exist yet, start a new one
	_, err = ExecuteDelayedChatResolutionAnalysisWorkflow(ctx, t.Temporal, DelayedChatResolutionAnalysisParams{
		ChatID:    chatID,
		ProjectID: projectID,
		OrgID:     orgID,
	})
	if err != nil {
		return fmt.Errorf("failed to start delayed analysis workflow: %w", err)
	}

	return nil
}

func ExecuteDelayedChatResolutionAnalysisWorkflow(ctx context.Context, temporalClient client.Client, params DelayedChatResolutionAnalysisParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:delayed-chat-resolution-analysis:%s", params.ChatID.String())
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    24 * time.Hour, // Max time to wait for inactivity
	}, DelayedChatResolutionAnalysisWorkflow, params)
}

func DelayedChatResolutionAnalysisWorkflow(ctx workflow.Context, params DelayedChatResolutionAnalysisParams) error {
	logger := workflow.GetLogger(ctx)

	// Set up signal channel for timer resets
	resetTimerChan := workflow.GetSignalChannel(ctx, SignalResetTimer)

	// Loop: wait for inactivity or timer reset
	for {
		selector := workflow.NewSelector(ctx)
		timerFired := false

		// Create a timer for the inactivity duration
		timer := workflow.NewTimer(ctx, ChatResolutionInactivityDuration)

		// Case 1: Timer fires (inactivity period completed)
		selector.AddFuture(timer, func(f workflow.Future) {
			timerFired = true
		})

		// Case 2: Signal received (new activity, reset timer)
		selector.AddReceive(resetTimerChan, func(c workflow.ReceiveChannel, more bool) {
			var signal interface{}
			c.Receive(ctx, &signal)
			logger.Info("Inactivity timer reset due to new activity",
				"chat_id", params.ChatID.String(),
			)
			// Timer will be reset on next loop iteration
		})

		// Wait for either event
		selector.Select(ctx)

		// If timer fired, break out and trigger analysis
		if timerFired {
			logger.Info("Inactivity period completed, triggering analysis",
				"chat_id", params.ChatID.String(),
				"duration_minutes", ChatResolutionInactivityDuration.Minutes(),
			)
			break
		}

		// Otherwise, signal was received, continue loop to restart timer
	}

	// Trigger the actual analysis workflow
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: fmt.Sprintf("v1:analyze-chat-resolutions:%s", params.ChatID.String()),
	})

	analysisParams := AnalyzeChatResolutionsParams(params)
	err := workflow.ExecuteChildWorkflow(childCtx, AnalyzeChatResolutionsWorkflow, analysisParams).Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to execute chat resolution analysis: %w", err)
	}

	return nil
}
