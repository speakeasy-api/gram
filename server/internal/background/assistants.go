package background

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"

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
	ProjectID string `json:"project_id"`
	ThreadID  string `json:"thread_id"`
}

// AssistantCoordinatorWorkflow is the per-assistant orchestrator that admits
// pending threads and fans out per-thread workflows. The body is supplied by
// the workflows PR; this stub exists so signal-with-start and worker
// registration can resolve the workflow by reference rather than by name.
func AssistantCoordinatorWorkflow(_ workflow.Context, _ AssistantCoordinatorWorkflowInput) error {
	return nil
}

// AssistantThreadWorkflow is the per-thread workflow that drives event
// processing on a runtime. See AssistantCoordinatorWorkflow for the stub
// rationale.
func AssistantThreadWorkflow(_ workflow.Context, _ AssistantThreadWorkflowInput) error {
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
	return signalAssistantCoordinator(ctx, temporalEnv.Client(), string(temporalEnv.Queue()), assistantID)
}

func SignalAssistantThread(ctx context.Context, temporalEnv *tenv.Environment, projectID, threadID uuid.UUID) error {
	if temporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}
	return signalAssistantThread(ctx, temporalEnv.Client(), string(temporalEnv.Queue()), projectID, threadID)
}

func signalAssistantCoordinator(ctx context.Context, temporalClient client.Client, taskQueue string, assistantID uuid.UUID) error {
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
		AssistantCoordinatorWorkflow,
		AssistantCoordinatorWorkflowInput{AssistantID: assistantID.String()},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start assistant coordinator workflow: %w", err)
	}
	return nil
}

func signalAssistantThread(ctx context.Context, temporalClient client.Client, taskQueue string, projectID, threadID uuid.UUID) error {
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
		AssistantThreadWorkflow,
		AssistantThreadWorkflowInput{
			ProjectID: projectID.String(),
			ThreadID:  threadID.String(),
		},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start assistant thread workflow: %w", err)
	}
	return nil
}
