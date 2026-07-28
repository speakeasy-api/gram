package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/plugins"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// ExecutePluginInitialPublishWorkflow kicks off the first-time GitHub publish
// for a newly created project's Default plugin. The rollout schedule (see
// plugin_generator_rollout.go) only republishes projects that already have a
// GitHub connection, so a brand new project needs this one-shot trigger to
// get its marketplace repo without waiting on a human to click Publish.
func ExecutePluginInitialPublishWorkflow(ctx context.Context, temporalEnv *tenv.Environment, input plugins.PublishProjectInput) (client.WorkflowRun, error) {
	return temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    fmt.Sprintf("v1:plugin-initial-publish/%s", input.ProjectID),
		TaskQueue:             string(temporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		// Must exceed the activity's worst-case retry budget (3 attempts x
		// 5m StartToCloseTimeout + backoff ~= 16m), or the workflow can expire
		// mid-retry and drop the final result.
		WorkflowRunTimeout: 20 * time.Minute,
	}, PluginInitialPublishWorkflow, input)
}

func PluginInitialPublishWorkflow(ctx workflow.Context, input plugins.PublishProjectInput) (*plugins.PublishProjectResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    1 * time.Minute,
		},
	})

	var a *Activities
	var result plugins.PublishProjectResult
	if err := workflow.ExecuteActivity(ctx, a.PublishPluginProject, input).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("publish plugin project: %w", err)
	}
	return &result, nil
}
