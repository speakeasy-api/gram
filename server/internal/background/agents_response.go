package background

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

type AgentsResponseWorkflowParams struct {
	OrgID     string
	ProjectID uuid.UUID
	Request   agents.ResponseRequest
}

func ExecuteAgentsResponseWorkflow(ctx context.Context, temporalClient client.Client, params AgentsResponseWorkflowParams) (client.WorkflowRun, error) {
	// Generate UUIDv7 for workflow ID, which will also be used as the response ID
	workflowID := uuid.Must(uuid.NewV7()).String()
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                string(TaskQueueMain),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowRunTimeout:       time.Minute * 60,
	}, AgentsResponseWorkflow, params)
}

func AgentsResponseWorkflow(ctx workflow.Context, params AgentsResponseWorkflowParams) (*agents.ResponseOutput, error) {
	var a *Activities

	logger := workflow.GetLogger(ctx)
	logger.Info("executing agents response workflow",
		"org_id", params.OrgID,
		"project_id", params.ProjectID.String())

	// Get workflow ID to use as response ID
	workflowInfo := workflow.GetInfo(ctx)
	responseID := workflowInfo.WorkflowExecution.ID

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	// Convert to activity input
	activityInput := activities.AgentsResponseInput{
		OrgID:      params.OrgID,
		ProjectID:  params.ProjectID,
		Request:    params.Request,
		ResponseID: responseID,
	}

	var activityOutput agents.ResponseOutput
	err := workflow.ExecuteActivity(
		ctx,
		a.AgentsResponse,
		activityInput,
	).Get(ctx, &activityOutput)
	if err != nil {
		logger.Error("failed to execute agents response activity", "error", err)
		return nil, err
	}

	return &activityOutput, nil
}
