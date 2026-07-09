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

// Tenant-scoped attribute_metrics_summaries backfill (Null-table staging
// pattern; the table mechanics live in the internal/mvbackfill driver, the
// MV-specific wiring in the activities package). The workflow is a
// two-phase, operator-gated rebuild:
//
// Phase 1 (automatic): pick an hour-aligned boundary, clear staging, replay
// the tenant's raw telemetry_logs in day chunks through the Null-engine feed
// (the deployed backfill MV performs the rollup into staging), then produce a
// staging-vs-live validation report.
//
// Gate: the workflow parks on SignalBackfillAttributeMetricsDecision until an
// operator has manually verified the invariants (the report is exposed via
// the QueryBackfillAttributeMetricsValidationReport query handler and in the
// Validate activity result in workflow history).
//
// Phase 2 (on approval): archive the live rows in the delete window, swap
// staging into live (synchronous delete + insert), clear staging. On
// rejection: clear staging and finish without touching the live table.
//
// Operate it with the Temporal CLI:
//
//	temporal workflow start --type BackfillAttributeMetricsSummariesWorkflow \
//	  --task-queue <queue> --workflow-id 'v1:backfill-attribute-metrics-summaries:<project-id>' \
//	  --input '{"project_id": "<project-id>"}'
//	temporal workflow query --workflow-id ... --type backfill-validation-report
//	temporal workflow signal --workflow-id ... \
//	  --name backfill-attribute-metrics-decision --input '{"approve": true}'
const (
	// SignalBackfillAttributeMetricsDecision carries the operator's
	// approve/abort verdict after manual validation of the staged rebuild.
	SignalBackfillAttributeMetricsDecision = "backfill-attribute-metrics-decision"

	// QueryBackfillAttributeMetricsValidationReport returns the
	// staging-vs-live validation stats (zero value until staging completes).
	QueryBackfillAttributeMetricsValidationReport = "backfill-validation-report"

	// backfillAttributeMetricsChunk is the staging chunk width. Day-sized
	// chunks keep single INSERT ... SELECTs bounded and make the loop
	// resumable at day granularity; raw retention is 90 days, so a full
	// rebuild is at most ~91 activities.
	backfillAttributeMetricsChunk = 24 * time.Hour
)

type BackfillAttributeMetricsSummariesParams struct {
	// ProjectID is the tenant (gram_project_id) whose summaries are rebuilt.
	ProjectID string `json:"project_id"`
}

type BackfillAttributeMetricsSummariesResult struct {
	ProjectID        string `json:"project_id"`
	BoundaryUnixNano int64  `json:"boundary_unix_nano"`
	RawRowCount      uint64 `json:"raw_row_count"`
	StagedChunks     int    `json:"staged_chunks"`

	Report *activities.ValidateAttributeMetricsBackfillResult `json:"report,omitempty"`

	// Committed reports whether the staged data was swapped into the live
	// table. False means the run finished without modifying live data (nothing
	// to backfill, or the operator aborted).
	Committed        bool   `json:"committed"`
	AbortReason      string `json:"abort_reason,omitempty"`
	ArchivedRowCount uint64 `json:"archived_row_count"`
	InsertedRowCount uint64 `json:"inserted_row_count"`
}

// BackfillAttributeMetricsDecision is the operator signal payload.
type BackfillAttributeMetricsDecision struct {
	Approve bool   `json:"approve"`
	Reason  string `json:"reason,omitempty"`
}

func ExecuteBackfillAttributeMetricsSummariesWorkflow(ctx context.Context, env *tenv.Environment, params BackfillAttributeMetricsSummariesParams) (client.WorkflowRun, error) {
	// One workflow ID per tenant: Temporal rejects a second concurrent run for
	// the same project while allowing re-runs once the previous one closed.
	id := fmt.Sprintf("v1:backfill-attribute-metrics-summaries:%s", params.ProjectID)
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, BackfillAttributeMetricsSummariesWorkflow, params)
}

// SignalBackfillAttributeMetricsSummariesDecision delivers the operator's
// approve/abort verdict to a waiting backfill workflow.
func SignalBackfillAttributeMetricsSummariesDecision(ctx context.Context, env *tenv.Environment, projectID string, decision BackfillAttributeMetricsDecision) error {
	id := fmt.Sprintf("v1:backfill-attribute-metrics-summaries:%s", projectID)
	if err := env.Client().SignalWorkflow(ctx, id, "", SignalBackfillAttributeMetricsDecision, decision); err != nil {
		return fmt.Errorf("signal backfill decision: %w", err)
	}
	return nil
}

func BackfillAttributeMetricsSummariesWorkflow(ctx workflow.Context, params BackfillAttributeMetricsSummariesParams) (*BackfillAttributeMetricsSummariesResult, error) {
	// Hour-aligned boundary: summary buckets are hourly, so everything below
	// the boundary is rebuilt from staging while buckets at/after it keep
	// being written by the live MV during the run — no gap, no double count.
	boundary := workflow.Now(ctx).UTC().Truncate(time.Hour)

	result := &BackfillAttributeMetricsSummariesResult{
		ProjectID:        params.ProjectID,
		BoundaryUnixNano: boundary.UnixNano(),
		RawRowCount:      0,
		StagedChunks:     0,
		Report:           nil,
		Committed:        false,
		AbortReason:      "",
		ArchivedRowCount: 0,
		InsertedRowCount: 0,
	}

	// Expose the validation report to `temporal workflow query` for the
	// operator gate; empty until the Validate activity completes.
	if err := workflow.SetQueryHandler(ctx, QueryBackfillAttributeMetricsValidationReport, func() (*activities.ValidateAttributeMetricsBackfillResult, error) {
		return result.Report, nil
	}); err != nil {
		return nil, fmt.Errorf("register validation report query handler: %w", err)
	}

	// Staging chunks and the commit mutation rewrite large parts; give each
	// call a wide budget and rely on heartbeats for liveness.
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	})

	var a *Activities

	var prep activities.PrepareAttributeMetricsBackfillResult
	if err := workflow.ExecuteActivity(ctx, a.PrepareAttributeMetricsBackfill, activities.PrepareAttributeMetricsBackfillParams{
		ProjectID:        params.ProjectID,
		BoundaryUnixNano: boundary.UnixNano(),
	}).Get(ctx, &prep); err != nil {
		return nil, fmt.Errorf("prepare backfill: %w", err)
	}
	result.RawRowCount = prep.RawRowCount
	if prep.RawRowCount == 0 {
		result.AbortReason = "no raw telemetry logs to rebuild from"
		return result, nil
	}

	// Replay oldest-first so a long run loses nothing to the rolling raw-log
	// TTL: the oldest (most at-risk) rows are staged first.
	for start := time.Unix(0, prep.MinTimeUnixNano).UTC().Truncate(backfillAttributeMetricsChunk); start.Before(boundary); start = start.Add(backfillAttributeMetricsChunk) {
		end := start.Add(backfillAttributeMetricsChunk)
		if end.After(boundary) {
			end = boundary
		}
		if err := workflow.ExecuteActivity(ctx, a.StageAttributeMetricsBackfillChunk, activities.StageAttributeMetricsBackfillChunkParams{
			ProjectID:    params.ProjectID,
			FromUnixNano: start.UnixNano(),
			ToUnixNano:   end.UnixNano(),
		}).Get(ctx, nil); err != nil {
			return nil, fmt.Errorf("stage chunk starting %s: %w", start.Format(time.RFC3339), err)
		}
		result.StagedChunks++
	}

	var report activities.ValidateAttributeMetricsBackfillResult
	if err := workflow.ExecuteActivity(ctx, a.ValidateAttributeMetricsBackfill, activities.ValidateAttributeMetricsBackfillParams{
		ProjectID:        params.ProjectID,
		BoundaryUnixNano: boundary.UnixNano(),
	}).Get(ctx, &report); err != nil {
		return nil, fmt.Errorf("validate backfill: %w", err)
	}
	result.Report = &report

	// Operator gate: park until a human has compared the staging and live
	// stats (and run any ad-hoc queries against the staging table) and signals
	// the verdict. The workflow ID is stable per tenant, so the signal needs
	// only the project ID.
	var decision BackfillAttributeMetricsDecision
	workflow.GetSignalChannel(ctx, SignalBackfillAttributeMetricsDecision).Receive(ctx, &decision)

	if !decision.Approve {
		result.AbortReason = decision.Reason
		if result.AbortReason == "" {
			result.AbortReason = "operator rejected the staged backfill"
		}
		if err := workflow.ExecuteActivity(ctx, a.CleanupAttributeMetricsBackfill, activities.CleanupAttributeMetricsBackfillParams{
			ProjectID: params.ProjectID,
		}).Get(ctx, nil); err != nil {
			return nil, fmt.Errorf("clean up staging after abort: %w", err)
		}
		return result, nil
	}

	// Archive completes before Commit starts, so the snapshot is taken from
	// untouched live rows even if Commit later retries mid-swap.
	var archive activities.ArchiveAttributeMetricsBackfillResult
	if err := workflow.ExecuteActivity(ctx, a.ArchiveAttributeMetricsBackfill, activities.ArchiveAttributeMetricsBackfillParams{
		ProjectID:        params.ProjectID,
		BoundaryUnixNano: boundary.UnixNano(),
		BackfillRunID:    workflow.GetInfo(ctx).WorkflowExecution.RunID,
	}).Get(ctx, &archive); err != nil {
		return nil, fmt.Errorf("archive live rows: %w", err)
	}
	result.ArchivedRowCount = archive.ArchivedRowCount

	var commit activities.CommitAttributeMetricsBackfillResult
	if err := workflow.ExecuteActivity(ctx, a.CommitAttributeMetricsBackfill, activities.CommitAttributeMetricsBackfillParams{
		ProjectID:        params.ProjectID,
		BoundaryUnixNano: boundary.UnixNano(),
	}).Get(ctx, &commit); err != nil {
		return nil, fmt.Errorf("commit backfill: %w", err)
	}
	result.Committed = true
	result.InsertedRowCount = commit.InsertedRowCount

	// Best-effort: staged rows are transient and Prepare re-clears them on the
	// next run, so a cleanup failure must not fail a committed backfill.
	if err := workflow.ExecuteActivity(ctx, a.CleanupAttributeMetricsBackfill, activities.CleanupAttributeMetricsBackfillParams{
		ProjectID: params.ProjectID,
	}).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Warn("failed to clean up staging after commit", "error", err)
	}

	return result, nil
}
