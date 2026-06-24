package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// policyEvalScanStartToCloseTimeout bounds the single scan activity, which
	// loops over the whole sample (up to max_messages) issuing judge calls. It
	// matches the realtime analyze-batch budget.
	policyEvalScanStartToCloseTimeout = 50 * time.Minute

	policyEvalGCScheduleID       = "v1:policy-eval-gc-schedule"
	policyEvalGCWorkflowID       = policyEvalGCScheduleID + "/scheduled"
	policyEvalGCInterval         = 1 * time.Hour
	policyEvalGCBatchSize  int32 = 100
)

// PolicyEvalRunWorkflowParams identifies the run a PolicyEvalRunWorkflow drives.
type PolicyEvalRunWorkflowParams struct {
	RunID     uuid.UUID
	ProjectID uuid.UUID
}

func evalRunWorkflowID(runID uuid.UUID) string {
	return "v1:policy-eval-run:" + runID.String()
}

// PolicyEvalRunWorkflow drives a single non-enforcing policy eval ("session
// replay"): it resolves the run's sample, scans it (writing findings to
// policy_eval_findings only), and rolls up the run statistics. It deliberately
// does NOT reuse RiskAnalysisCoordinatorWorkflow — that path marks messages
// analyzed, writes risk_results, and appends to the outbox, all of which an
// eval must avoid.
//
// Cancellation: the API marks the run 'cancelled' in the DB and cancels this
// workflow. The status guards on Complete/Fail (running-only / pending,running
// -only) make those post-cancel writes no-ops, so cancellation needs no special
// rollback here beyond not overwriting the 'cancelled' state.
func PolicyEvalRunWorkflow(ctx workflow.Context, params PolicyEvalRunWorkflowParams) error {
	logger := workflow.GetLogger(ctx)
	ref := risk_analysis.PolicyEvalRunRef{RunID: params.RunID, ProjectID: params.ProjectID}

	bookkeepingCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
		},
	})
	// The scan writes findings as it goes and is not idempotent, so it must not
	// be retried — a retry would double-write. A transient failure fails the run
	// and the operator re-runs. It runs on the dedicated risk-analysis queue
	// (where the heavy scanner deps live) so eval scans don't starve the main
	// queue.
	scanCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           RiskAnalysisTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName)),
		StartToCloseTimeout: policyEvalScanStartToCloseTimeout,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	var a *Activities

	var sampleSize int
	if err := workflow.ExecuteActivity(bookkeepingCtx, a.SelectPolicyEvalSample, ref).Get(bookkeepingCtx, &sampleSize); err != nil {
		return finishFailedPolicyEvalRun(ctx, a, ref, fmt.Sprintf("select sample: %v", err))
	}

	var res risk_analysis.PolicyEvalScanResult
	if err := workflow.ExecuteActivity(scanCtx, a.RunPolicyEvalScan, ref).Get(scanCtx, &res); err != nil {
		if temporal.IsCanceledError(err) {
			logger.Info("policy eval run canceled", "run_id", params.RunID.String())
			return nil
		}
		return finishFailedPolicyEvalRun(ctx, a, ref, fmt.Sprintf("scan: %v", err))
	}

	if err := workflow.ExecuteActivity(bookkeepingCtx, a.CompletePolicyEvalRun, ref, res).Get(bookkeepingCtx, nil); err != nil {
		logger.Error("complete policy eval run failed", "error", err.Error())
		return fmt.Errorf("complete policy eval run: %w", err)
	}

	logger.Info("policy eval run completed",
		"run_id", params.RunID.String(),
		"messages_scanned", res.MessagesScanned,
		"findings", res.FindingsCount,
	)
	return nil
}

// finishFailedPolicyEvalRun records the failure reason on the run and ends the
// workflow cleanly (the failure is captured in the run's status/error, not the
// workflow result). Uses a disconnected context so the mark-failed write still
// runs if the workflow itself is being canceled.
func finishFailedPolicyEvalRun(ctx workflow.Context, a *Activities, ref risk_analysis.PolicyEvalRunRef, reason string) error {
	disconnected, cancel := workflow.NewDisconnectedContext(ctx)
	defer cancel()
	failCtx := workflow.WithActivityOptions(disconnected, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	if err := workflow.ExecuteActivity(failCtx, a.FailPolicyEvalRun, ref, reason).Get(failCtx, nil); err != nil {
		workflow.GetLogger(ctx).Error("mark policy eval run failed", "error", err.Error())
	}
	return nil
}

// TemporalPolicyEvalRunner starts and cancels PolicyEvalRunWorkflow executions.
// It is the implementation behind the risk service's PolicyEvalRunner seam.
type TemporalPolicyEvalRunner struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

func (r *TemporalPolicyEvalRunner) Start(ctx context.Context, runID, projectID uuid.UUID) error {
	wfID := evalRunWorkflowID(runID)
	_, err := r.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    wfID,
		TaskQueue:             string(r.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, PolicyEvalRunWorkflow, PolicyEvalRunWorkflowParams{RunID: runID, ProjectID: projectID})

	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	switch {
	case errors.As(err, &alreadyStarted):
		return nil
	case err != nil:
		return fmt.Errorf("start policy eval run workflow: %w", err)
	}

	r.Logger.DebugContext(ctx, "policy eval run workflow started",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}

func (r *TemporalPolicyEvalRunner) Cancel(ctx context.Context, runID, projectID uuid.UUID) error {
	wfID := evalRunWorkflowID(runID)
	var notFound *serviceerror.NotFound
	if err := r.TemporalEnv.Client().CancelWorkflow(ctx, wfID, ""); err != nil && !errors.As(err, &notFound) {
		return fmt.Errorf("cancel policy eval run workflow: %w", err)
	}
	r.Logger.DebugContext(ctx, "policy eval run workflow cancel requested",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}

// PolicyEvalGCWorkflow deletes past-retention eval runs in batches (cascading to
// their findings, which carry raw match text). Modeled on OutboxGCWorkflow.
func PolicyEvalGCWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var a *Activities

	for {
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			return workflow.NewContinueAsNewError(ctx, PolicyEvalGCWorkflow)
		}

		var rows int64
		if err := workflow.ExecuteActivity(ctx, a.GCExpiredPolicyEvalRuns, policyEvalGCBatchSize).Get(ctx, &rows); err != nil {
			return fmt.Errorf("gc expired policy eval runs: %w", err)
		}

		workflow.GetLogger(ctx).Info("policy eval gc batch completed", "rows_deleted", rows)

		if rows < int64(policyEvalGCBatchSize) {
			return nil // all expired runs processed — schedule re-runs next interval
		}
	}
}

func AddPolicyEvalGCSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{{Every: policyEvalGCInterval}},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 policyEvalGCWorkflowID,
		Workflow:           PolicyEvalGCWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: 10 * time.Minute,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:      policyEvalGCScheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec:    spec,
		Action:  action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := sc.GetHandle(ctx, policyEvalGCScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing policy eval gc schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create policy eval gc schedule: %w", err)
	}

	return nil
}
