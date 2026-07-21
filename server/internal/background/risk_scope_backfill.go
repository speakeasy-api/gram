package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// ExecuteBackfillRiskDetectionScopesWorkflow starts the one-shot migration of
// legacy risk policy scoping fields (message_types, scope_include,
// scope_exempt) into per-category detection scopes. Triggered manually (via
// the Temporal UI/CLI) once per environment after the code that stops
// enforcing the legacy fields is deployed.
func ExecuteBackfillRiskDetectionScopesWorkflow(ctx context.Context, env *tenv.Environment) (client.WorkflowRun, error) {
	return env.Client().ExecuteWorkflow(ctx,
		client.StartWorkflowOptions{
			ID:                       fmt.Sprintf("v1:backfill-risk-detection-scopes:%d", time.Now().Unix()),
			TaskQueue:                string(env.Queue()),
			WorkflowExecutionTimeout: time.Hour,
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		BackfillRiskDetectionScopesWorkflow,
	)
}

func BackfillRiskDetectionScopesWorkflow(ctx workflow.Context) error {
	var a *Activities
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			MaximumInterval:    time.Minute,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
	})

	if err := workflow.ExecuteActivity(ctx, a.BackfillRiskDetectionScopes).Get(ctx, nil); err != nil {
		return fmt.Errorf("backfill risk detection scopes: %w", err)
	}
	return nil
}
