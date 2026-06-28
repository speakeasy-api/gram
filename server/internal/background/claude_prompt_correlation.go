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
	correlateClaudePromptsActivityStartToCloseTimeout = 30 * time.Second
	correlateClaudePromptsWorkflowRunTimeout          = 2 * time.Minute
	// Worst-case retry window for one activity: three 30s attempts plus the
	// 5s and 10s backoffs between them.
	correlateClaudePromptsActivityWorstCaseRetryWindow = 105 * time.Second
)

type CorrelateClaudePromptsParams struct {
	ProjectID              uuid.UUID
	ChatID                 uuid.UUID
	SessionID              string
	AfterMessageSeq        int64
	AfterEventSequence     int64
	AfterEventTimeUnixNano int64
}

func ExecuteCorrelateClaudePromptsWorkflow(ctx context.Context, env *tenv.Environment, params CorrelateClaudePromptsParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:correlate-claude-prompts:%s:%s", params.ProjectID.String(), params.ChatID.String())
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    correlateClaudePromptsWorkflowRunTimeout,
	}, CorrelateClaudePromptsWorkflow, params)
}

func CorrelateClaudePromptsWorkflow(ctx workflow.Context, params CorrelateClaudePromptsParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: correlateClaudePromptsActivityStartToCloseTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
		},
	})

	var a *Activities
	for {
		var result activities.CorrelateClaudePromptsResult
		if err := workflow.ExecuteActivity(
			ctx,
			a.CorrelateClaudePrompts,
			activities.CorrelateClaudePromptsArgs(params),
		).Get(ctx, &result); err != nil {
			return fmt.Errorf("correlate Claude prompts: %w", err)
		}
		if !result.HasMore {
			return nil
		}
		params.AfterMessageSeq = result.AfterMessageSeq
		params.AfterEventSequence = result.AfterEventSequence
		params.AfterEventTimeUnixNano = result.AfterEventTimeUnixNano
		if shouldContinueCorrelateClaudePromptsAsNew(ctx) {
			return workflow.NewContinueAsNewError(ctx, CorrelateClaudePromptsWorkflow, params)
		}
	}
}

func shouldContinueCorrelateClaudePromptsAsNew(ctx workflow.Context) bool {
	info := workflow.GetInfo(ctx)
	if info.GetContinueAsNewSuggested() {
		return true
	}

	runTimeout := info.WorkflowRunTimeout
	if runTimeout == 0 {
		runTimeout = correlateClaudePromptsWorkflowRunTimeout
	}
	if runTimeout == 0 || info.WorkflowStartTime.IsZero() {
		return false
	}

	elapsed := workflow.Now(ctx).Sub(info.WorkflowStartTime)
	return elapsed+correlateClaudePromptsActivityWorstCaseRetryWindow >= runTimeout
}
