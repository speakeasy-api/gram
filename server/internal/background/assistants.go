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

type AssistantRuntimeWarmupWorkflowInput struct {
	AssistantID string `json:"assistant_id"`
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
		if result.BootstrappedRuntime {
			// v2 cold admit fanned out only this thread to avoid a concurrent
			// Ensure race; the runtime row is now active so signal the
			// coordinator to admit any siblings that were held back.
			if err := workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
				AssistantID: result.AssistantID,
			}).Get(ctx, nil); err != nil {
				return err
			}
		} else if result.ProcessedAnyEvent {
			// Active count dropped, so re-evaluate admission for any
			// siblings the cap held back at the previous cycle.
			v := workflow.GetVersion(ctx, "coordinator-kick-on-processed", workflow.DefaultVersion, 1)
			if v == 1 {
				if err := workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
					AssistantID: result.AssistantID,
				}).Get(ctx, nil); err != nil {
					return err
				}
			}
		}

		var waitFor time.Duration
		if result.WarmUntil != "" {
			warmUntil, err := time.Parse(time.RFC3339Nano, result.WarmUntil)
			if err != nil {
				return fmt.Errorf("parse assistant warm_until: %w", err)
			}
			waitFor = max(warmUntil.Sub(workflow.Now(ctx).UTC()), 0)
		}

		v := workflow.GetVersion(ctx, "expire-toctou-revert", workflow.DefaultVersion, 2)
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
				WarmTTLSeconds: 0,
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

		if v == 1 {
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
		if timerFired {
			return nil
		}
	}

	drainSignals(signalCh)
	return workflow.NewContinueAsNewError(ctx, AssistantThreadWorkflow, input)
}

// AssistantRuntimeWarmupWorkflow eagerly boots a freshly created assistant's
// runtime (Fly app + VM) so the first turn doesn't pay the cold-start cost.
// The coordinator is signalled after warmup regardless of outcome: any
// threads that arrived while the runtime row was `starting` were held back
// by admit and need a kick now that the row is active (or failed and
// reapable).
//
// When warmup booted the runtime, this workflow also owns the warm timer —
// a runtime that never receives a turn has no thread workflow to expire it
// and the machine config disables Fly autostop, so without this the VM
// would run until the inactivity janitor. A single expire attempt is
// enough: if it reports a turn slipped in, that turn's thread workflow
// exists and runs its own warm timer from here on.
func AssistantRuntimeWarmupWorkflow(ctx workflow.Context, input AssistantRuntimeWarmupWorkflowInput) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// Cold boot covers app create, IP allocation, DNS propagation, image
		// pull and the health probe — minutes on a bad day, so cap generously.
		StartToCloseTimeout:    10 * time.Minute,
		ScheduleToStartTimeout: 30 * time.Second,
		// Heartbeat deadline so a worker crash mid-boot is detected in
		// minutes, not the full StartToClose window.
		HeartbeatTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})

	signalCoordinator := func() error {
		return workflow.ExecuteActivity(ctx, a.SignalAssistantCoordinator, activities.SignalAssistantCoordinatorInput{
			AssistantID: input.AssistantID,
		}).Get(ctx, nil)
	}

	var result activities.WarmAssistantRuntimeResult
	warmErr := workflow.ExecuteActivity(ctx, a.WarmAssistantRuntime, activities.WarmAssistantRuntimeInput{
		AssistantID: input.AssistantID,
	}).Get(ctx, &result)

	// A failed kick must not abort before the expiry below when a runtime
	// was booted — nothing else would stop the VM. Hold the error and
	// surface it at the end.
	sigErr := signalCoordinator()
	if warmErr != nil {
		return warmErr
	}
	if !result.Booted {
		return sigErr
	}

	if err := workflow.NewTimer(ctx, time.Duration(result.WarmTTLSeconds)*time.Second).Get(ctx, nil); err != nil {
		return err
	}
	var expire activities.ExpireAssistantThreadRuntimeResult
	if err := workflow.ExecuteActivity(ctx, a.ExpireWarmupAssistantRuntime, activities.ExpireWarmupAssistantRuntimeInput{
		AssistantID:    input.AssistantID,
		ProjectID:      result.ProjectID,
		WarmTTLSeconds: result.WarmTTLSeconds,
	}).Get(ctx, &expire); err != nil {
		return err
	}
	// Kick the coordinator on both outcomes: a stop freed the assistant's
	// runtime slot, and a revert (a turn slipped in — its thread workflow
	// owns the lifecycle now) still left admit skipping any threads that
	// were enqueued while the row sat in `expiring`.
	if err := signalCoordinator(); err != nil {
		return err
	}
	return sigErr
}

func assistantCoordinatorWorkflowID(assistantID uuid.UUID) string {
	return "v1:assistant-coordinator:" + assistantID.String()
}

func assistantThreadWorkflowID(threadID uuid.UUID) string {
	return "v1:assistant-thread:" + threadID.String()
}

func assistantRuntimeWarmupWorkflowID(assistantID uuid.UUID) string {
	return "v1:assistant-runtime-warmup:" + assistantID.String()
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

func (s *AssistantWorkflowSignaler) StartRuntimeWarmup(ctx context.Context, assistantID uuid.UUID) error {
	_, err := s.TemporalEnv.Client().ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:        assistantRuntimeWarmupWorkflowID(assistantID),
			TaskQueue: string(s.TemporalEnv.Queue()),
			// Coalesce concurrent starts onto the running warmup; allow a
			// fresh run after a prior one completed (e.g. managed assistant
			// disabled and re-enabled).
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		},
		AssistantRuntimeWarmupWorkflow,
		AssistantRuntimeWarmupWorkflowInput{AssistantID: assistantID.String()},
	)
	if err != nil {
		return fmt.Errorf("start assistant runtime warmup workflow: %w", err)
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
