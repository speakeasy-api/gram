package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ProcessNameMappingWorkflowParams struct {
	ServerName    string
	ToolCallAttrs map[string]any
	OrgID         string
	ProjectID     string
}

func ExecuteProcessNameMappingWorkflow(ctx context.Context, env *tenv.Environment, params ProcessNameMappingWorkflowParams) (client.WorkflowRun, error) {
	id := fmt.Sprintf("v1:process-name-mapping:%s:%s", params.ServerName, params.ProjectID)
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       id,
		TaskQueue:                string(env.Queue()),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowRunTimeout:       5 * time.Minute,
	}, ProcessNameMappingWorkflow, params)
}

func ProcessNameMappingWorkflow(ctx workflow.Context, params ProcessNameMappingWorkflowParams) error {
	// This can stay nil/unassigned. Temporal just uses this to get activity names.
	// The actual activities are registered in the CLI layer (cmd/gram/worker.go).
	var a *Activities

	logger := workflow.GetLogger(ctx)

	// Activity for generating name mapping with LLM
	generateCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	})

	var mappingResult activities.GenerateNameMappingResult
	err := workflow.ExecuteActivity(
		generateCtx,
		a.GenerateNameMapping,
		activities.GenerateNameMappingArgs{
			ServerName:    params.ServerName,
			ToolCallAttrs: params.ToolCallAttrs,
			OrgID:         params.OrgID,
			ProjectID:     params.ProjectID,
		},
	).Get(generateCtx, &mappingResult)
	if err != nil {
		return fmt.Errorf("generate name mapping: %w", err)
	}

	// If no mapping was found, we're done (not an error)
	if mappingResult.MappedName == "" {
		logger.Info("No name mapping generated for server", "server_name", params.ServerName)
		return nil
	}

	logger.Info("Generated name mapping",
		"server_name", params.ServerName,
		"mapped_name", mappingResult.MappedName,
	)

	// Activity for updating ClickHouse records
	// Use longer timeout and more retries since mutations can be slow
	updateCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			MaximumInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
	})

	err = workflow.ExecuteActivity(
		updateCtx,
		a.UpdateClickHouseToolSource,
		activities.UpdateClickHouseToolSourceArgs{
			ProjectID: params.ProjectID,
			OldSource: mappingResult.OriginalName,
			NewSource: mappingResult.MappedName,
		},
	).Get(updateCtx, nil)
	if err != nil {
		return fmt.Errorf("update ClickHouse tool source: %w", err)
	}

	logger.Info("Successfully completed name mapping workflow",
		"server_name", params.ServerName,
		"mapped_name", mappingResult.MappedName,
		"project_id", params.ProjectID,
	)

	return nil
}
