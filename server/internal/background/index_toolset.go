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
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
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
	env *tenv.Environment,
	params IndexToolsetParams,
) (client.WorkflowRun, error) {
	id := fmt.Sprintf(
		"v1:index-toolset:%s:%s",
		params.ProjectID,
		params.ToolsetSlug,
	)
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       id,
		TaskQueue:                string(env.Queue()),
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:       2 * time.Minute,
	}, IndexToolsetWorkflow, params)
}

func IndexToolsetWorkflow(
	ctx workflow.Context,
	params IndexToolsetParams,
) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 45 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
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
