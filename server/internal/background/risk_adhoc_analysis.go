package background

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

const (
	// RiskAdhocProgressQueryType names the Temporal query exposing run progress.
	RiskAdhocProgressQueryType = "risk-adhoc-progress"

	// riskAdhocFetchPageSize bounds one workflow run's fan-out; a full page
	// triggers ContinueAsNew with the keyset cursor advanced.
	riskAdhocFetchPageSize int32 = 10_000
)

// RiskAdhocAnalysisParams describes an operator-triggered re-scan of a
// project's chat messages over an explicit time window. Unlike the live
// coordinator this ignores the risk_analyzed_at watermark and never advances
// it, so ad-hoc runs and live scanning stay independent.
type RiskAdhocAnalysisParams struct {
	ProjectID uuid.UUID
	// RiskPolicyID scopes the run to one enabled policy; unset means all
	// enabled policies for the project.
	RiskPolicyID uuid.NullUUID
	// From (inclusive) and To (exclusive) bound the message window by
	// creation time.
	From time.Time
	To   time.Time
	// Cursor and Progress carry accumulated state across ContinueAsNew.
	// Zero-valued when triggering a new run.
	Cursor   uuid.UUID
	Progress RiskAdhocProgress
}

// RiskAdhocProgress is both the live query response and the workflow result.
type RiskAdhocProgress struct {
	TotalMessages      int64
	DispatchedMessages int64
	ProcessedMessages  int64
	Findings           int64
	BatchesCompleted   int64
	BatchesFailed      int64
	Policies           int
}

// RiskAdhocAnalysisWorkflow pages messages in [From, To) and fans out
// AnalyzeBatch per (policy, batch) onto the dedicated ad-hoc task queue,
// keeping backfill load off the live risk-analysis queue. Batch failures are
// counted rather than failing the run; fetch failures fail it loudly.
func RiskAdhocAnalysisWorkflow(ctx workflow.Context, params RiskAdhocAnalysisParams) (RiskAdhocProgress, error) {
	logger := workflow.GetLogger(ctx)
	progress := params.Progress

	if err := workflow.SetQueryHandler(ctx, RiskAdhocProgressQueryType, func() (RiskAdhocProgress, error) {
		return progress, nil
	}); err != nil {
		return progress, fmt.Errorf("set progress query handler: %w", err)
	}

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	analyzeBatchOpts := activityOpts
	analyzeBatchOpts.TaskQueue = RiskAdhocAnalysisTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName))
	analyzeBatchOpts.StartToCloseTimeout = analyzeBatchStartToCloseTimeout
	analyzeBatchOpts.HeartbeatTimeout = 60 * time.Second
	analyzeBatchCtx := workflow.WithActivityOptions(ctx, analyzeBatchOpts)

	idLowerBound := uuidv7.LowerBound(params.From)
	idUpperBound := uuidv7.LowerBound(params.To)

	var a *Activities

	if params.Cursor == uuid.Nil {
		if err := workflow.ExecuteActivity(ctx, a.CountAdhocAnalysisMessages, risk_analysis.CountAdhocArgs{
			ProjectID:    params.ProjectID,
			IDLowerBound: idLowerBound,
			IDUpperBound: idUpperBound,
		}).Get(ctx, &progress.TotalMessages); err != nil {
			return progress, fmt.Errorf("count adhoc analysis messages: %w", err)
		}
	}

	var fetchResult risk_analysis.FetchAdhocResult
	if err := workflow.ExecuteActivity(ctx, a.FetchAdhocAnalysisMessages, risk_analysis.FetchAdhocArgs{
		ProjectID:    params.ProjectID,
		RiskPolicyID: params.RiskPolicyID,
		IDLowerBound: idLowerBound,
		IDUpperBound: idUpperBound,
		IDCursor:     params.Cursor,
		BatchLimit:   riskAdhocFetchPageSize,
	}).Get(ctx, &fetchResult); err != nil {
		return progress, fmt.Errorf("fetch adhoc analysis messages: %w", err)
	}
	progress.Policies = len(fetchResult.Policies)

	if len(fetchResult.MessageIDs) == 0 || len(fetchResult.Policies) == 0 {
		return progress, nil
	}

	var futures []workflow.Future
	for _, policy := range fetchResult.Policies {
		for _, batch := range chunkUUIDs(fetchResult.MessageIDs, riskCoordinatorBatchSize) {
			f := workflow.ExecuteActivity(analyzeBatchCtx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
				ProjectID:        params.ProjectID,
				OrganizationID:   policy.OrganizationID,
				RiskPolicyID:     policy.ID,
				PolicyVersion:    policy.Version,
				MessageIDs:       batch,
				Sources:          policy.Sources,
				MessageTypes:     policy.MessageTypes,
				PresidioEntities: policy.PresidioEntities,
				CustomRuleIds:    policy.CustomRuleIds,
				// Derived authoritatively from the refetched policy inside
				// AnalyzeBatch.Do; zero values here are placeholders.
				PresidioScoreThreshold: 0,
				ApprovedEmailDomains:   nil,
				BuiltinPresetsEnabled:  false,
				DetectionScopes:        nil,
			})
			futures = append(futures, f)
		}
	}

	progress.DispatchedMessages += int64(len(fetchResult.MessageIDs))

	for _, f := range futures {
		var batchResult risk_analysis.AnalyzeBatchResult
		if err := f.Get(ctx, &batchResult); err != nil {
			progress.BatchesFailed++
			logger.Error("adhoc analyze batch failed", "error", err.Error())
			continue
		}
		progress.BatchesCompleted++
		progress.ProcessedMessages += int64(batchResult.Processed)
		progress.Findings += int64(batchResult.Findings)
	}

	if len(fetchResult.MessageIDs) == int(riskAdhocFetchPageSize) {
		params.Cursor = fetchResult.MessageIDs[len(fetchResult.MessageIDs)-1]
		params.Progress = progress
		return progress, workflow.NewContinueAsNewError(ctx, RiskAdhocAnalysisWorkflow, params)
	}

	return progress, nil
}

// RiskAdhocWorkflowID returns the workflow id for a project's ad-hoc run.
// One run per project may be in flight at a time.
func RiskAdhocWorkflowID(projectID uuid.UUID) string {
	return fmt.Sprintf("v1:risk-adhoc:%s", projectID.String())
}

// StartRiskAdhocAnalysis launches an ad-hoc analysis run. A trigger while a
// run is already in flight for the project fails with
// serviceerror.WorkflowExecutionAlreadyStarted.
func StartRiskAdhocAnalysis(ctx context.Context, temporalEnv *tenv.Environment, params RiskAdhocAnalysisParams) (client.WorkflowRun, error) {
	run, err := temporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    RiskAdhocWorkflowID(params.ProjectID),
		TaskQueue:             string(temporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, RiskAdhocAnalysisWorkflow, params)
	if err != nil {
		return nil, fmt.Errorf("start risk adhoc analysis workflow: %w", err)
	}
	return run, nil
}
