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

type CorrelateClaudePromptsParams struct {
	ProjectID uuid.UUID
	ChatID    uuid.UUID
	SessionID string
}

func ExecuteCorrelateClaudePromptsWorkflow(ctx context.Context, env *tenv.Environment, params CorrelateClaudePromptsParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:correlate-claude-prompts:%s:%s", params.ProjectID.String(), params.ChatID.String())
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    2 * time.Minute,
	}, CorrelateClaudePromptsWorkflow, params)
}

func CorrelateClaudePromptsWorkflow(ctx workflow.Context, params CorrelateClaudePromptsParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(
		ctx,
		a.CorrelateClaudePrompts,
		activities.CorrelateClaudePromptsArgs(params),
	).Get(ctx, nil); err != nil {
		return fmt.Errorf("correlate Claude prompts: %w", err)
	}
	return nil
}
