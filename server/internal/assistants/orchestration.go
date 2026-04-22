package assistants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	AssistantCoordinatorWorkflowName = "AssistantCoordinatorWorkflow"
	AssistantThreadWorkflowName      = "AssistantThreadWorkflow"

	SignalAssistantCoordinatorKick = "assistant_coordinator_kick"
	SignalAssistantThreadKick      = "assistant_thread_kick"
)

type CoordinatorWorkflowInput struct {
	AssistantID string `json:"assistant_id"`
}

type ThreadWorkflowInput struct {
	ThreadID string `json:"thread_id"`
}

type Dispatcher struct {
	core        *ServiceCore
	temporalEnv *tenv.Environment
}

func NewDispatcher(core *ServiceCore, temporalEnv *tenv.Environment) *Dispatcher {
	return &Dispatcher{
		core:        core,
		temporalEnv: temporalEnv,
	}
}

func (d *Dispatcher) Kind() string {
	return bgtriggers.TargetKindAssistant
}

func (d *Dispatcher) Dispatch(ctx context.Context, task bgtriggers.Task) error {
	if d.core == nil {
		return fmt.Errorf("assistant service core is not configured")
	}

	result, err := d.core.EnqueueTriggerTask(ctx, task)
	if err != nil {
		return fmt.Errorf("enqueue assistant trigger task: %w", err)
	}
	if !result.EventCreated || result.AssistantID == uuid.Nil {
		return nil
	}

	if err := SignalAssistantCoordinator(ctx, d.temporalEnv, result.AssistantID); err != nil {
		return fmt.Errorf("signal assistant coordinator: %w", err)
	}

	return nil
}

func assistantCoordinatorWorkflowID(assistantID uuid.UUID) string {
	return "v1:assistant-coordinator:" + assistantID.String()
}

func assistantThreadWorkflowID(threadID uuid.UUID) string {
	return "v1:assistant-thread:" + threadID.String()
}

func SignalAssistantCoordinator(ctx context.Context, temporalEnv *tenv.Environment, assistantID uuid.UUID) error {
	if temporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}
	return SignalAssistantCoordinatorWithClient(ctx, temporalEnv.Client(), string(temporalEnv.Queue()), assistantID)
}

func SignalAssistantThread(ctx context.Context, temporalEnv *tenv.Environment, threadID uuid.UUID) error {
	if temporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}
	return SignalAssistantThreadWithClient(ctx, temporalEnv.Client(), string(temporalEnv.Queue()), threadID)
}

func SignalAssistantCoordinatorWithClient(ctx context.Context, temporalClient client.Client, taskQueue string, assistantID uuid.UUID) error {
	if temporalClient == nil {
		return fmt.Errorf("temporal client is not configured")
	}
	_, err := temporalClient.SignalWithStartWorkflow(
		ctx,
		assistantCoordinatorWorkflowID(assistantID),
		SignalAssistantCoordinatorKick,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    assistantCoordinatorWorkflowID(assistantID),
			TaskQueue:             taskQueue,
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		AssistantCoordinatorWorkflowName,
		CoordinatorWorkflowInput{AssistantID: assistantID.String()},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start assistant coordinator workflow: %w", err)
	}
	return nil
}

func SignalAssistantThreadWithClient(ctx context.Context, temporalClient client.Client, taskQueue string, threadID uuid.UUID) error {
	if temporalClient == nil {
		return fmt.Errorf("temporal client is not configured")
	}
	_, err := temporalClient.SignalWithStartWorkflow(
		ctx,
		assistantThreadWorkflowID(threadID),
		SignalAssistantThreadKick,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    assistantThreadWorkflowID(threadID),
			TaskQueue:             taskQueue,
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		AssistantThreadWorkflowName,
		ThreadWorkflowInput{ThreadID: threadID.String()},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start assistant thread workflow: %w", err)
	}
	return nil
}
