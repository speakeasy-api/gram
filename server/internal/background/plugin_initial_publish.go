package background

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// Deprecated: PluginInitialPublishWorkflow is superseded by
// PluginPublishWorkflowDebounced (see plugin_publish.go), which serializes and
// debounces every publish for a project under one workflow ID instead of
// racing a fresh execution per trigger. Nothing starts this any more; it stays
// registered for one release so executions in flight across the deploy can
// finish, and can be deleted once none are running.
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
