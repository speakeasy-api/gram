package background

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	risk_policy "github.com/speakeasy-api/gram/server/internal/background/activities/risk_policy"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const riskPolicyCleanupTimeout = 30 * time.Minute

// RiskPolicyCleanupParams identifies the policy whose results should be deleted.
type RiskPolicyCleanupParams struct {
	ProjectID uuid.UUID
	PolicyID  uuid.UUID
}

// RiskPolicyCleanupWorkflow deletes risk_results for a soft-deleted policy.
func RiskPolicyCleanupWorkflow(ctx workflow.Context, params RiskPolicyCleanupParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: riskPolicyCleanupTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	})

	var a *Activities
	return workflow.ExecuteActivity(ctx, a.CleanRiskPolicyResults, risk_policy.CleanArgs{
		ProjectID: params.ProjectID,
		PolicyID:  params.PolicyID,
	}).Get(ctx, nil)
}

func riskPolicyCleanupWorkflowID(policyID uuid.UUID) string {
	return "risk-policy-cleanup:" + policyID.String()
}

// TemporalRiskPolicyResultsCleaner starts the cleanup workflow after a policy
// is soft-deleted. Best-effort: a failed trigger is logged, not fatal.
type TemporalRiskPolicyResultsCleaner struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

func (c *TemporalRiskPolicyResultsCleaner) Clean(ctx context.Context, projectID, policyID uuid.UUID) error {
	wfID := riskPolicyCleanupWorkflowID(policyID)

	_, err := c.TemporalEnv.Client().ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(c.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_TERMINATE_IF_RUNNING,
		},
		RiskPolicyCleanupWorkflow,
		RiskPolicyCleanupParams{ProjectID: projectID, PolicyID: policyID},
	)
	if err != nil {
		return fmt.Errorf("start risk policy cleanup: %w", err)
	}

	c.Logger.DebugContext(ctx, "risk policy cleanup started",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}

// CleanAll starts results cleanup for policies a committed transaction
// deleted. It logs failures instead of returning them because the delete has
// already committed. A nil TemporalEnv, as in services built without Temporal,
// skips the cleanup.
func (c *TemporalRiskPolicyResultsCleaner) CleanAll(ctx context.Context, projectID uuid.UUID, policyIDs []uuid.UUID) {
	if c.TemporalEnv == nil {
		return
	}
	for _, policyID := range policyIDs {
		if err := c.Clean(ctx, projectID, policyID); err != nil {
			c.Logger.ErrorContext(ctx, "trigger risk policy results cleanup",
				attr.SlogProjectID(projectID.String()),
				attr.SlogRiskPolicyID(policyID.String()),
				attr.SlogError(err),
			)
		}
	}
}
