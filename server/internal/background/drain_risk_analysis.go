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
)

const (
	// SignalRiskAnalysisRequested wakes the drain workflow on new messages or
	// policy updates.
	SignalRiskAnalysisRequested = "risk-analysis-requested"

	// drainFetchLimit is how many unanalyzed message IDs to fetch per round.
	drainFetchLimit int32 = 20_000

	// drainBatchSize is how many messages each AnalyzeBatch activity processes.
	drainBatchSize = 1_000

	// drainMaxConcurrency is the maximum number of AnalyzeBatch activities
	// running in parallel.
	drainMaxConcurrency = 20
)

// DrainRiskAnalysisParams identifies the policy this workflow drains.
type DrainRiskAnalysisParams struct {
	ProjectID     uuid.UUID
	RiskPolicyID  uuid.UUID
	PolicyVersion int64
	Sources       []string
}

// DrainRiskAnalysisWorkflow is a perpetual "one-man queue" for a single risk
// policy. It drains all unanalyzed messages, then sleeps until signaled.
// ContinueAsNew keeps history bounded.
func DrainRiskAnalysisWorkflow(ctx workflow.Context, params DrainRiskAnalysisParams) error {
	logger := workflow.GetLogger(ctx)
	signalCh := workflow.GetSignalChannel(ctx, SignalRiskAnalysisRequested)

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

	// Local activity options for the lightweight fetch query.
	localCtx := workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    15 * time.Second,
		},
	})

	var a *Activities

	// ── Drain loop ──────────────────────────────────────────────────────
	for {
		var fetchResult risk_analysis.FetchUnanalyzedResult
		err := workflow.ExecuteLocalActivity(localCtx, a.FetchUnanalyzedMessages, risk_analysis.FetchUnanalyzedArgs{
			ProjectID:     params.ProjectID,
			RiskPolicyID:  params.RiskPolicyID,
			PolicyVersion: params.PolicyVersion,
			BatchLimit:    drainFetchLimit,
		}).Get(ctx, &fetchResult)
		if err != nil {
			logger.Error("fetch unanalyzed message IDs", "error", err.Error())
			break
		}

		if len(fetchResult.MessageIDs) == 0 {
			break
		}

		// Fan out batches with bounded concurrency.
		batches := chunkUUIDs(fetchResult.MessageIDs, drainBatchSize)
		pending := make([]workflow.Future, 0, min(len(batches), drainMaxConcurrency))

		for _, batch := range batches {
			if len(pending) >= drainMaxConcurrency {
				if err := pending[0].Get(ctx, nil); err != nil {
					logger.Error("analyze batch failed", "error", err.Error())
				}
				pending = pending[1:]
			}

			f := workflow.ExecuteActivity(ctx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
				ProjectID:     params.ProjectID,
				RiskPolicyID:  params.RiskPolicyID,
				PolicyVersion: params.PolicyVersion,
				MessageIDs:    batch,
				Sources:       params.Sources,
			})
			pending = append(pending, f)
		}

		for _, f := range pending {
			if err := f.Get(ctx, nil); err != nil {
				logger.Error("analyze batch failed", "error", err.Error())
			}
		}

		// ContinueAsNew if history is getting large.
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			drainSignals(signalCh)
			return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, params)
		}
	}

	// ── Sleep until signaled ────────────────────────────────────────────
	// Check for queued signals before blocking.
	if drainSignals(signalCh) {
		return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, params)
	}

	// Block until a signal arrives, then ContinueAsNew.
	signalCh.Receive(ctx, nil)
	drainSignals(signalCh)
	return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, params)
}

// drainSignals consumes all queued signals. Returns true if at least one was consumed.
func drainSignals(ch workflow.ReceiveChannel) bool {
	gotAny := false
	for ch.ReceiveAsync(nil) {
		gotAny = true
	}
	return gotAny
}

func chunkUUIDs(ids []uuid.UUID, size int) [][]uuid.UUID {
	var chunks [][]uuid.UUID
	for i := 0; i < len(ids); i += size {
		end := min(i+size, len(ids))
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}

// ── Signaler ────────────────────────────────────────────────────────────

// RiskAnalysisSignaler sends signals to drain workflows.
type RiskAnalysisSignaler interface {
	SignalNewMessages(ctx context.Context, params DrainRiskAnalysisParams) error
	GetWorkflowStatus(ctx context.Context, policyID uuid.UUID) (string, error)
}

// TemporalRiskAnalysisSignaler implements RiskAnalysisSignaler using Temporal.
type TemporalRiskAnalysisSignaler struct {
	TemporalEnv *tenv.Environment
}

func (s *TemporalRiskAnalysisSignaler) SignalNewMessages(ctx context.Context, params DrainRiskAnalysisParams) error {
	if s.TemporalEnv == nil {
		return nil
	}

	wfID := drainWorkflowID(params.RiskPolicyID)
	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		wfID,
		SignalRiskAnalysisRequested,
		nil,
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		DrainRiskAnalysisWorkflow,
		params,
	)
	if err != nil {
		return fmt.Errorf("signal-with-start drain risk analysis: %w", err)
	}
	return nil
}

func (s *TemporalRiskAnalysisSignaler) GetWorkflowStatus(ctx context.Context, policyID uuid.UUID) (string, error) {
	if s.TemporalEnv == nil {
		return "not_started", nil
	}

	wfID := drainWorkflowID(policyID)
	desc, err := s.TemporalEnv.Client().DescribeWorkflowExecution(ctx, wfID, "")
	if err != nil {
		return "not_started", nil //nolint:nilerr // workflow may not exist
	}

	if desc.WorkflowExecutionInfo.GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return "running", nil
	}
	return "not_started", nil
}

func drainWorkflowID(policyID uuid.UUID) string {
	return fmt.Sprintf("v1:drain-risk-analysis:%s", policyID.String())
}
