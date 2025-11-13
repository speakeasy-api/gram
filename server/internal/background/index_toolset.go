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

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

type IndexToolsetParams struct {
	ProjectID   uuid.UUID
	ToolsetSlug types.Slug
}

type IndexToolsetClient struct {
	Temporal client.Client
}

func ExecuteIndexToolset(
	ctx context.Context,
	temporalClient client.Client,
	params IndexToolsetParams,
) (client.WorkflowRun, error) {
	id := fmt.Sprintf(
		"v1:index-toolset:%s:%s",
		params.ProjectID,
		params.ToolsetSlug,
	)
	return temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    30 * time.Minute,
	}, IndexToolsetWorkflow, params)
}

func IndexToolsetWorkflow(
	ctx workflow.Context,
	params IndexToolsetParams,
) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	return workflow.ExecuteActivity(
		ctx,
		a.GenerateToolsetEmbeddings,
		activities.GenerateToolsetEmbeddingsInput{
			ProjectID:   params.ProjectID,
			ToolsetSlug: params.ToolsetSlug,
		},
	).Get(ctx, nil)
}
