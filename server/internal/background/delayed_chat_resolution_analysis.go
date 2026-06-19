package background

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/workflow"
)

const (
	ChatResolutionInactivityDuration = 30 * time.Minute
	SignalResetTimer                 = "reset-timer"
	SignalResolveImmediately         = "resolve-immediately"
)

type DelayedChatResolutionAnalysisParams struct {
	ChatID    uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
	APIKeyID  string
}

func DelayedChatResolutionAnalysisWorkflow(ctx workflow.Context, params DelayedChatResolutionAnalysisParams) error {
	logger := workflow.GetLogger(ctx)

	// Set up signal channels
	resetTimerChan := workflow.GetSignalChannel(ctx, SignalResetTimer)
	resolveImmediatelyChan := workflow.GetSignalChannel(ctx, SignalResolveImmediately)

	// Loop: wait for inactivity, timer reset, or immediate resolution
	for {
		selector := workflow.NewSelector(ctx)
		timerFired := false
		resolveImmediately := false

		// Create a timer for the inactivity duration
		timer := workflow.NewTimer(ctx, ChatResolutionInactivityDuration)

		// Case 1: Timer fires (inactivity period completed)
		selector.AddFuture(timer, func(f workflow.Future) {
			timerFired = true
		})

		// Case 2: Signal received (new activity, reset timer)
		selector.AddReceive(resetTimerChan, func(c workflow.ReceiveChannel, more bool) {
			var signal any
			c.Receive(ctx, &signal)
			logger.Info("Inactivity timer reset due to new activity",
				"chat_id", params.ChatID.String(),
			)
			// Timer will be reset on next loop iteration
		})

		// Case 3: Signal received (resolve immediately)
		selector.AddReceive(resolveImmediatelyChan, func(c workflow.ReceiveChannel, more bool) {
			var signal any
			c.Receive(ctx, &signal)
			logger.Info("Immediate resolution requested, triggering analysis",
				"chat_id", params.ChatID.String(),
			)
			resolveImmediately = true
		})

		// Wait for either event
		selector.Select(ctx)

		// If timer fired or immediate resolution requested, break out and trigger analysis
		if timerFired {
			logger.Info("Inactivity period completed, triggering analysis",
				"chat_id", params.ChatID.String(),
				"duration_minutes", ChatResolutionInactivityDuration.Minutes(),
			)
			break
		}

		if resolveImmediately {
			break
		}

		// Otherwise, reset signal was received, continue loop to restart timer
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
