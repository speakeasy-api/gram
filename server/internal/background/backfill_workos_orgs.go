package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// BackfillWorkOSOrganizationsWorkflow snapshots the current WorkOS state for all linked orgs.
// It must be triggered manually via the Temporal UI or CLI — it is not scheduled.
// Useful after a DB wipe or to reconcile data without relying on the event cursor.
func BackfillWorkOSOrganizationsWorkflow(ctx workflow.Context) error {
	var a *Activities

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	var workosOrgIDs []string
	if err := workflow.ExecuteActivity(ctx, a.GetAllWorkOSLinkedOrganizations).Get(ctx, &workosOrgIDs); err != nil {
		return fmt.Errorf("get workos linked organizations: %w", err)
	}

	for _, workosOrgID := range workosOrgIDs {
		if err := workflow.ExecuteActivity(ctx, a.BackfillWorkOSOrg, activities.BackfillWorkOSOrgParams{
			WorkOSOrgID: workosOrgID,
		}).Get(ctx, nil); err != nil {
			workflow.GetLogger(ctx).Warn("backfill failed for workos org", "workos_org_id", workosOrgID, "error", err)
		}
	}

	return nil
}

// TriggerBackfillWorkOSOrganizations starts a one-shot backfill workflow via the Temporal client.
// Intended to be called from admin tooling or scripts.
func TriggerBackfillWorkOSOrganizations(ctx context.Context, temporalEnv *tenv.Environment) (client.WorkflowRun, error) {
	run, err := temporalEnv.Client().ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:                       "v1:backfill-workos-organizations",
			TaskQueue:                string(temporalEnv.Queue()),
			WorkflowExecutionTimeout: 30 * time.Minute,
			WorkflowIDReusePolicy:    0, // ALLOW_DUPLICATE — each trigger is independent
		},
		BackfillWorkOSOrganizationsWorkflow,
	)
	if err != nil {
		return nil, fmt.Errorf("trigger backfill workos organizations: %w", err)
	}
	return run, nil
}
