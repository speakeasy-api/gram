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

// reconcileSecondSweepDelay covers the ClickHouse ingest race: the finding
// writer annotates rows from an exclusion-set cache with a 60s TTL (plus a
// ~1s async-insert flush), so rows consumed inside that window after an
// exclusion change can land with stale flags AFTER the first sweep already
// scanned their partition. A single delayed re-run converges them; it is
// near-free because the reconcile's predicates only touch rows whose latest
// state actually differs.
const reconcileSecondSweepDelay = 2 * time.Minute

// RiskExclusionReconcileParams identifies the exclusion to reconcile.
type RiskExclusionReconcileParams struct {
	ProjectID   uuid.UUID
	ExclusionID uuid.UUID
}

// RiskExclusionReconcileWorkflow flags/unflags stored findings to match an
// exclusion's current state, then re-runs once after a short delay to catch
// findings ingested with stale exclusion flags during the writer cache's TTL
// window. The activity reads the exclusion's live state, so even if a newer
// reconcile supersedes this one (TERMINATE_IF_RUNNING — including during the
// sleep) the result converges.
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
	args := risk_exclusion.ReconcileArgs{
		ProjectID:   params.ProjectID,
		ExclusionID: params.ExclusionID,
	}
	if err := workflow.ExecuteActivity(ctx, a.ReconcileExclusion, args).Get(ctx, nil); err != nil {
		return fmt.Errorf("reconcile exclusion: %w", err)
	}

	if err := workflow.Sleep(ctx, reconcileSecondSweepDelay); err != nil {
		return fmt.Errorf("sleep before reconcile second sweep: %w", err)
	}

	if err := workflow.ExecuteActivity(ctx, a.ReconcileExclusion, args).Get(ctx, nil); err != nil {
		return fmt.Errorf("reconcile exclusion second sweep: %w", err)
	}
	return nil
}

func reconcileWorkflowID(exclusionID uuid.UUID) string {
	return "risk-exclusion-reconcile:" + exclusionID.String()
}

// The consumer-side reconciler interface lives in the risk service
// (risk.RiskExclusionReconciler); TemporalRiskExclusionReconciler is its
// concrete implementation, wired in cmd/gram/start.go.

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
