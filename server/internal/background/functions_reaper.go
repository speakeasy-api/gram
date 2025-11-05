package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

type FunctionsReaperWorkflowParams struct {
	Scope activities.FunctionsReaperScope

	ProjectID uuid.NullUUID
}

type FunctionsReaperWorkflowResult struct {
	AppsReaped int
	Errors     int
}

func ExecuteProjectFunctionsReaperChildWorkflow(ctx workflow.Context, projectID uuid.UUID) (workflow.ChildWorkflowFuture, error) {
	return workflow.ExecuteChildWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       "v1:functions-reaper:" + projectID.String(),
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 5,
	}, FunctionsReaperWorkflow, FunctionsReaperWorkflowParams{
		Scope:     activities.FunctionsReaperScopeProject,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
	}), nil
}

func ExecuteFunctionsReaperWorkflow(ctx context.Context, temporalClient client.Client, params FunctionsReaperWorkflowParams) (client.WorkflowRun, error) {
	// Use a fixed workflow ID so that only one reaper workflow can run at a time
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       "v1:functions-reaper",
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 10,
	}, FunctionsReaperWorkflow, params)
}

func FunctionsReaperWorkflow(ctx workflow.Context, params FunctionsReaperWorkflowParams) (*FunctionsReaperWorkflowResult, error) {
	// This can stay nil/unassigned. Temporal just uses this to get activity names.
	// The actual activities are registered in the CLI layer (cmd/gram/worker.go).
	var a *Activities

	logger := workflow.GetLogger(ctx)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	})

	var result activities.ReapFlyAppsResult
	err := workflow.ExecuteActivity(
		ctx,
		a.ReapFlyApps,
		activities.ReapFlyAppsRequest{
			Scope:     params.Scope,
			ProjectID: params.ProjectID,
		},
	).Get(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to reap functions: %w", err)
	}

	logger.Info("functions reaper completed",
		"apps_reaped", result.Reaped,
		"errors", result.Errors,
	)

	return &FunctionsReaperWorkflowResult{
		AppsReaped: result.Reaped,
		Errors:     result.Errors,
	}, nil
}
