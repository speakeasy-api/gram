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
