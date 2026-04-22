package background

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

// assistantCoordinatorMaxIterations bounds how many kicks a single workflow
// run handles before ContinueAsNew. Temporal's per-workflow history limit is
// ~51.2k events; each iteration adds roughly 6, so 500 leaves a wide margin
// while keeping restart churn negligible.
const assistantCoordinatorMaxIterations = 500

// assistantCoordinatorIdleTimeout lets a coordinator exit when nothing has
// signalled it in a while. Prevents the workflow index from growing
// without bound as assistants get created and abandoned. A fresh kick
// re-bootstraps via SignalWithStart.
const assistantCoordinatorIdleTimeout = 1 * time.Hour

func AssistantCoordinatorWorkflow(ctx workflow.Context, input assistants.CoordinatorWorkflowInput) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	admitPending := func() error {
		var admitted activities.AdmitAssistantThreadsResult
		if err := workflow.ExecuteActivity(ctx, a.AdmitAssistantThreads, activities.AdmitAssistantThreadsInput{
			AssistantID: input.AssistantID,
		}).Get(ctx, &admitted); err != nil {
			return err
		}
		for _, threadID := range admitted.ThreadIDs {
			if err := workflow.ExecuteActivity(ctx, a.SignalAssistantThread, activities.SignalAssistantThreadInput{
				ThreadID: threadID,
			}).Get(ctx, nil); err != nil {
				return err
			}
		}
		return nil
	}

	signalCh := workflow.GetSignalChannel(ctx, assistants.SignalAssistantCoordinatorKick)

	for range assistantCoordinatorMaxIterations {
		idle := false
		selector := workflow.NewSelector(ctx)
		selector.AddFuture(workflow.NewTimer(ctx, assistantCoordinatorIdleTimeout), func(workflow.Future) {
			idle = true
		})
		selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, more bool) {
			var ignored struct{}
			c.Receive(ctx, &ignored)
		})
		selector.Select(ctx)

		if idle {
			return nil
		}

		if err := admitPending(); err != nil {
			return err
		}
	}

	return workflow.NewContinueAsNewError(ctx, AssistantCoordinatorWorkflow, input)
}

func AssistantThreadWorkflow(ctx workflow.Context, input assistants.ThreadWorkflowInput) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// Agent turns may iterate through many tool calls (MCP round-trips +
		// LLM turns). Cap generously so we don't cancel a running turn and
		// cause the event to be re-claimed while the runner is still working.
		StartToCloseTimeout: 20 * time.Minute,
		// If no worker picks up the scheduled task quickly, retry rather than
		// waiting StartToClose. Prevents workflows from wedging when a worker
		// dies after a task is dispatched but before it acks STARTED.
		ScheduleToStartTimeout: 30 * time.Second,
		// Bound the full attempt budget so a silently stuck worker eventually
		// surfaces a terminal error instead of extending indefinitely.
		ScheduleToCloseTimeout: 25 * time.Minute,
		// Heartbeat deadline so mid-turn worker crashes are detected in
		// minutes, not the 20-minute StartToClose window.
		HeartbeatTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	signalCh := workflow.GetSignalChannel(ctx, assistants.SignalAssistantThreadKick)

	for {
		var result activities.ProcessAssistantThreadResult
		if err := workflow.ExecuteActivity(ctx, a.ProcessAssistantThread, activities.ProcessAssistantThreadInput{
			ThreadID: input.ThreadID,
		}).Get(ctx, &result); err != nil {
			return err
		}

		if result.AssistantID == "" {
			return nil
		}
		if result.RetryAdmission {
			return workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
				AssistantID: result.AssistantID,
			}).Get(ctx, nil)
		}
		if !result.RuntimeActive {
			return nil
		}

		warmUntil, err := time.Parse(time.RFC3339Nano, result.WarmUntil)
		if err != nil {
			return fmt.Errorf("parse assistant warm_until: %w", err)
		}
		waitFor := max(warmUntil.Sub(workflow.Now(ctx).UTC()), 0)

		expired := false
		selector := workflow.NewSelector(ctx)
		selector.AddFuture(workflow.NewTimer(ctx, waitFor), func(workflow.Future) {
			expired = true
		})
		selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, more bool) {
			var ignored struct{}
			c.Receive(ctx, &ignored)
		})
		selector.Select(ctx)

		if !expired {
			continue
		}

		if err := workflow.ExecuteActivity(ctx, a.ExpireAssistantThreadRuntime, activities.ExpireAssistantThreadRuntimeInput{
			ThreadID: input.ThreadID,
		}).Get(ctx, nil); err != nil {
			return err
		}
		if err := workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
			AssistantID: result.AssistantID,
		}).Get(ctx, nil); err != nil {
			return err
		}
		return nil
	}
}
