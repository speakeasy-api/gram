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
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const reconcileStartToCloseTimeout = 30 * time.Minute

// RiskExclusionReconcileParams identifies the exclusion to reconcile.
type RiskExclusionReconcileParams struct {
	ProjectID   uuid.UUID
	ExclusionID uuid.UUID
}

// RiskExclusionReconcileWorkflow flags/unflags stored findings to match an
// exclusion's current state. The activity reads the exclusion's live state, so
// even if a newer reconcile supersedes this one the result converges.
func RiskExclusionReconcileWorkflow(ctx workflow.Context, params RiskExclusionReconcileParams) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: reconcileStartToCloseTimeout,
		HeartbeatTimeout:    60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	})

	var a *Activities
	return workflow.ExecuteActivity(ctx, a.ReconcileExclusion, risk_exclusion.ReconcileArgs{
		ProjectID:   params.ProjectID,
		ExclusionID: params.ExclusionID,
	}).Get(ctx, nil)
}

func reconcileWorkflowID(exclusionID uuid.UUID) string {
	return "risk-exclusion-reconcile:" + exclusionID.String()
}

// RiskExclusionReconciler triggers the retroactive reconcile for an exclusion.
type RiskExclusionReconciler interface {
	Reconcile(ctx context.Context, projectID, exclusionID uuid.UUID) error
}

// TemporalRiskExclusionReconciler starts the reconcile workflow. A new trigger
// terminates any in-flight run for the same exclusion so the latest config
// always wins (update/delete supersede an earlier create/update sweep).
type TemporalRiskExclusionReconciler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

func (r *TemporalRiskExclusionReconciler) Reconcile(ctx context.Context, projectID, exclusionID uuid.UUID) error {
	wfID := reconcileWorkflowID(exclusionID)

	_, err := r.TemporalEnv.Client().ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(r.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_TERMINATE_IF_RUNNING,
		},
		RiskExclusionReconcileWorkflow,
		RiskExclusionReconcileParams{ProjectID: projectID, ExclusionID: exclusionID},
	)
	if err != nil {
		return fmt.Errorf("start risk exclusion reconcile: %w", err)
	}

	r.Logger.DebugContext(ctx, "risk exclusion reconcile started",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}
