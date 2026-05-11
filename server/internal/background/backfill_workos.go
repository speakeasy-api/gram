package background

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func ExecuteBackfillWorkOSWorkflow(ctx context.Context, env *tenv.Environment) (client.WorkflowRun, error) {
	return env.Client().ExecuteWorkflow(ctx,
		client.StartWorkflowOptions{
			ID:                       fmt.Sprintf("v1:backfill-workos:%d", time.Now().Unix()),
			TaskQueue:                string(env.Queue()),
			WorkflowExecutionTimeout: 2 * time.Hour,
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		BackfillWorkOSWorkflow,
	)
}

func BackfillWorkOSWorkflow(ctx workflow.Context) error {
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

	var orgIDs []string
	if err := workflow.ExecuteActivity(ctx, a.ListWorkOSOrganizations).Get(ctx, &orgIDs); err != nil {
		return fmt.Errorf("list WorkOS organizations: %w", err)
	}

	if err := workflow.ExecuteActivity(ctx, a.BackfillWorkOSGlobalRoles).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("global role backfill failed", "error", err)
	}

	for _, orgID := range orgIDs {
		if err := workflow.ExecuteActivity(ctx, a.BackfillWorkOSOrganization, activities.BackfillWorkOSOrganizationParams{
			WorkOSOrganizationID: orgID,
		}).Get(ctx, nil); err != nil {
			workflow.GetLogger(ctx).Warn("WorkOS organization backfill failed", "workos_org_id", orgID, "error", err)
		}
	}

	return nil
}
