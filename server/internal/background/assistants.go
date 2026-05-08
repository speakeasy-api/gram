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

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	SignalAssistantCoordinatorKick = "assistant_coordinator_kick"
	SignalAssistantThreadKick      = "assistant_thread_kick"
)

type AssistantCoordinatorWorkflowInput struct {
	AssistantID string `json:"assistant_id"`
}

type AssistantThreadWorkflowInput struct {
	ThreadID  string `json:"thread_id"`
	ProjectID string `json:"project_id"`
}

// assistantCoordinatorMaxIterations bounds how many kicks a single workflow
// run handles before ContinueAsNew. Temporal's per-workflow history limit is
// ~51.2k events; each iteration adds roughly 6, so 500 leaves a wide margin
// while keeping restart churn negligible.
const assistantCoordinatorMaxIterations = 500

// assistantThreadMaxIterations bounds the thread workflow's process->wait->kick
// loop before ContinueAsNew. Each iteration adds ~6-8 events; the warm-TTL
// expiry path normally exits the workflow before this caps, but a thread that
// keeps receiving kicks before warm expiry would otherwise grow history
// unbounded toward Temporal's ~51.2k limit.
const assistantThreadMaxIterations = 500

// assistantCoordinatorIdleTimeout lets a coordinator exit when nothing has
// signalled it in a while. Prevents the workflow index from growing
// without bound as assistants get created and abandoned. A fresh kick
// re-bootstraps via SignalWithStart.
const assistantCoordinatorIdleTimeout = 1 * time.Hour

// assistantRetryAdmissionBackoff prevents setup/runtime failures that return
// RetryAdmission from hot-looping through coordinator admission. The activity
// returns nil in these cases, so Temporal's activity retry policy is not in
// play.
const assistantRetryAdmissionBackoff = 30 * time.Second

func AssistantCoordinatorWorkflow(ctx workflow.Context, input AssistantCoordinatorWorkflowInput) error {
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
				ThreadID:  threadID,
				ProjectID: admitted.ProjectID,
			}).Get(ctx, nil); err != nil {
				return err
			}
		}
		return nil
	}

	signalCh := workflow.GetSignalChannel(ctx, SignalAssistantCoordinatorKick)

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

	drainSignals(signalCh)
	return workflow.NewContinueAsNewError(ctx, AssistantCoordinatorWorkflow, input)
}

func AssistantThreadWorkflow(ctx workflow.Context, input AssistantThreadWorkflowInput) error {
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

	signalCh := workflow.GetSignalChannel(ctx, SignalAssistantThreadKick)

	for range assistantThreadMaxIterations {
		var result activities.ProcessAssistantThreadResult
		if err := workflow.ExecuteActivity(ctx, a.ProcessAssistantThread, activities.ProcessAssistantThreadInput{
			ThreadID:  input.ThreadID,
			ProjectID: input.ProjectID,
		}).Get(ctx, &result); err != nil {
			return err
		}

		if result.AssistantID == "" {
			return nil
		}
		if result.RetryAdmission {
			if err := workflow.NewTimer(ctx, assistantRetryAdmissionBackoff).Get(ctx, nil); err != nil {
				return err
			}
			return workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
				AssistantID: result.AssistantID,
			}).Get(ctx, nil)
		}
		if !result.RuntimeActive {
			return nil
		}

		var waitFor time.Duration
		if result.WarmUntil != "" {
			warmUntil, err := time.Parse(time.RFC3339Nano, result.WarmUntil)
			if err != nil {
				return fmt.Errorf("parse assistant warm_until: %w", err)
			}
			waitFor = max(warmUntil.Sub(workflow.Now(ctx).UTC()), 0)
		}

		// Workflows that started on the previous code (single-shot timer +
		// expire + signal) replay through DefaultVersion to keep history
		// deterministic. New starts use v1, which adds a re-arm loop so the
		// expire activity can revert to active when its post-CAS status poll
		// finds a turn slipped in past the warm timer.
		v := workflow.GetVersion(ctx, "expire-toctou-revert", workflow.DefaultVersion, 1)
		if v == workflow.DefaultVersion {
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
				ThreadID:       input.ThreadID,
				ProjectID:      input.ProjectID,
				WarmTTLSeconds: 0, // v0 disables the revert path; activity falls through to Stop.
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

		runtimeStopped := false
		for {
			timerFired := false
			selector := workflow.NewSelector(ctx)
			selector.AddFuture(workflow.NewTimer(ctx, waitFor), func(workflow.Future) {
				timerFired = true
			})
			selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, more bool) {
				var ignored struct{}
				c.Receive(ctx, &ignored)
			})
			selector.Select(ctx)

			if !timerFired {
				break
			}

			var expireResult activities.ExpireAssistantThreadRuntimeResult
			if err := workflow.ExecuteActivity(ctx, a.ExpireAssistantThreadRuntime, activities.ExpireAssistantThreadRuntimeInput{
				ThreadID:       input.ThreadID,
				ProjectID:      input.ProjectID,
				WarmTTLSeconds: result.WarmTTLSeconds,
			}).Get(ctx, &expireResult); err != nil {
				return err
			}
			if expireResult.Stopped {
				runtimeStopped = true
				break
			}
			waitFor = time.Duration(expireResult.RemainingSeconds) * time.Second
		}

		if !runtimeStopped {
			continue
		}

		if err := workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
			AssistantID: result.AssistantID,
		}).Get(ctx, nil); err != nil {
			return err
		}
		return nil
	}

	drainSignals(signalCh)
	return workflow.NewContinueAsNewError(ctx, AssistantThreadWorkflow, input)
}

func assistantCoordinatorWorkflowID(assistantID uuid.UUID) string {
	return "v1:assistant-coordinator:" + assistantID.String()
}

func assistantThreadWorkflowID(threadID uuid.UUID) string {
	return "v1:assistant-thread:" + threadID.String()
}

type AssistantWorkflowSignaler struct {
	TemporalEnv *tenv.Environment
}

func (s *AssistantWorkflowSignaler) SignalCoordinator(ctx context.Context, assistantID uuid.UUID) error {
	wfID := assistantCoordinatorWorkflowID(assistantID)
	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		wfID,
		SignalAssistantCoordinatorKick,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		AssistantCoordinatorWorkflow,
		AssistantCoordinatorWorkflowInput{AssistantID: assistantID.String()},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start assistant coordinator workflow: %w", err)
	}
	return nil
}

func (s *AssistantWorkflowSignaler) SignalThread(ctx context.Context, threadID, projectID uuid.UUID) error {
	wfID := assistantThreadWorkflowID(threadID)
	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		wfID,
		SignalAssistantThreadKick,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		AssistantThreadWorkflow,
		AssistantThreadWorkflowInput{ThreadID: threadID.String(), ProjectID: projectID.String()},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start assistant thread workflow: %w", err)
	}
	return nil
}
