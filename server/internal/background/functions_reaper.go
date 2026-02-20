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
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

type FunctionsReaperWorkflowParams struct {
	Scope activities.FunctionsReaperScope

	ProjectID uuid.NullUUID
}

type FunctionsReaperWorkflowResult struct {
	AppsReaped int
	Errors     int
}

func ExecuteProjectFunctionsReaperWorkflow(ctx context.Context, env *tenv.Environment, projectID uuid.UUID) (client.WorkflowRun, error) {
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       "v1:functions-reaper:" + projectID.String(),
		TaskQueue:                string(env.Queue()),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 10,
	}, FunctionsReaperWorkflow, FunctionsReaperWorkflowParams{
		Scope:     activities.FunctionsReaperScopeProject,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
	})
}

func FunctionsReaperWorkflow(ctx workflow.Context, params FunctionsReaperWorkflowParams) (*FunctionsReaperWorkflowResult, error) {
	// This can stay nil/unassigned. Temporal just uses this to get activity names.
	// The actual activities are registered in the CLI layer (cmd/gram/worker.go).
	var a *Activities

	logger := workflow.GetLogger(ctx)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    1 * time.Minute,
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
