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
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/throttle"
)

const (
	SignalRiskAnalysisRequested = "risk-analysis-requested"

	// riskAnalysisLookback bounds FetchUnanalyzedMessageIDs to recent messages.
	// Messages older than this are not (re-)analyzed after the initial pass.
	riskAnalysisLookback = 2 * time.Hour

	riskCoordinatorFetchLimit int32 = 20_000
	riskCoordinatorBatchSize  int   = 100

	analyzeBatchStartToCloseTimeout = 50 * time.Minute
)

// RiskAnalysisCoordinatorParams identifies the project this coordinator runs for.
type RiskAnalysisCoordinatorParams struct {
	ProjectID uuid.UUID
}

// RiskAnalysisCoordinatorWorkflow is a per-project coordinator that:
//  1. Fetches all active policies and unanalyzed message IDs (within lookback).
//  2. Fans out AnalyzeBatch activities across all policy×batch combinations.
//  3. Fans in, then marks all fetched messages as analyzed.
//
// It sleeps until signaled (SignalRiskAnalysisRequested) and uses
// ContinueAsNew to keep history bounded. Backfills and policy-version
// rescans are handled out-of-band; this workflow is best-effort only.
func RiskAnalysisCoordinatorWorkflow(ctx workflow.Context, params RiskAnalysisCoordinatorParams) error {
	logger := workflow.GetLogger(ctx)
	signalCh := workflow.GetSignalChannel(ctx, SignalRiskAnalysisRequested)

	// Drain any signal that triggered this run so the end-of-cycle check
	// doesn't immediately ContinueAsNew for our own start signal.
	drainSignals(signalCh)

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
	analyzeBatchOpts.TaskQueue = RiskAnalysisTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName))
	analyzeBatchOpts.StartToCloseTimeout = analyzeBatchStartToCloseTimeout
	analyzeBatchOpts.HeartbeatTimeout = 60 * time.Second
	analyzeBatchCtx := workflow.WithActivityOptions(ctx, analyzeBatchOpts)

	idLowerBound := risk_analysis.UUIDv7LowerBound(workflow.Now(ctx).Add(-riskAnalysisLookback))

	var a *Activities
	var fetchResult risk_analysis.FetchUnanalyzedResult
	if err := workflow.ExecuteActivity(ctx, a.FetchUnanalyzedMessages, risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    params.ProjectID,
		IDLowerBound: idLowerBound,
		BatchLimit:   riskCoordinatorFetchLimit,
	}).Get(ctx, &fetchResult); err != nil {
		logger.Error("fetch unanalyzed messages failed", "error", err.Error())
	}

	if len(fetchResult.MessageIDs) > 0 && len(fetchResult.Policies) > 0 {
		// Fan-out: one activity per (policy, batch).
		var futures []workflow.Future
		for _, policy := range fetchResult.Policies {
			for _, batch := range chunkUUIDs(fetchResult.MessageIDs, riskCoordinatorBatchSize) {
				f := workflow.ExecuteActivity(analyzeBatchCtx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
					ProjectID:            params.ProjectID,
					OrganizationID:       policy.OrganizationID,
					RiskPolicyID:         policy.ID,
					PolicyVersion:        policy.Version,
					MessageIDs:           batch,
					Sources:              policy.Sources,
					InputTypes:           policy.InputTypes,
					PresidioEntities:     policy.PresidioEntities,
					PromptInjectionRules: policy.PromptInjectionRules,
					CustomRuleIds:        policy.CustomRuleIds,
				})
				futures = append(futures, f)
			}
		}

		// Fan-in: collect results; log errors but do not abort.
		for _, f := range futures {
			if err := f.Get(ctx, nil); err != nil {
				logger.Error("analyze batch failed", "error", err.Error())
			}
		}

		// Mark all fetched messages analyzed (best-effort).
		if err := workflow.ExecuteActivity(ctx, a.MarkMessagesAnalyzed, risk_analysis.MarkMessagesAnalyzedArgs{
			ProjectID:  params.ProjectID,
			MessageIDs: fetchResult.MessageIDs,
		}).Get(ctx, nil); err != nil {
			logger.Error("mark messages analyzed failed", "error", err.Error())
		}
	}

	// If signals arrived while processing, ContinueAsNew to pick them up.
	// Otherwise complete — SignalWithStart will restart on the next event.
	if drainSignals(signalCh) {
		return workflow.NewContinueAsNewError(ctx, RiskAnalysisCoordinatorWorkflow, params)
	}
	return nil
}

func coordinatorWorkflowID(projectID uuid.UUID) string {
	return fmt.Sprintf("v1:risk-analysis:%s", projectID.String())
}

func chunkUUIDs(ids []uuid.UUID, size int) [][]uuid.UUID {
	var chunks [][]uuid.UUID
	for i := 0; i < len(ids); i += size {
		end := min(i+size, len(ids))
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}

// drainSignals consumes all queued signals. Returns true if at least one was consumed.
func drainSignals(ch workflow.ReceiveChannel) bool {
	gotAny := false
	for ch.ReceiveAsync(nil) {
		gotAny = true
	}
	return gotAny
}

// ── Signaler ────────────────────────────────────────────────────────────────

// RiskAnalysisSignaler sends signals to the per-project coordinator workflow.
type RiskAnalysisSignaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
}

// TemporalRiskAnalysisSignaler implements RiskAnalysisSignaler using Temporal.
type TemporalRiskAnalysisSignaler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

func (s *TemporalRiskAnalysisSignaler) Signal(ctx context.Context, projectID uuid.UUID) error {
	wfID := coordinatorWorkflowID(projectID)

	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		wfID,
		SignalRiskAnalysisRequested,
		struct{}{},
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		RiskAnalysisCoordinatorWorkflow,
		RiskAnalysisCoordinatorParams{ProjectID: projectID},
	)
	if err != nil {
		return fmt.Errorf("signal-with-start coordinator: %w", err)
	}

	s.Logger.DebugContext(ctx, "risk coordinator signal sent",
		attr.SlogProjectID(projectID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}

// ── Throttled Signaler ───────────────────────────────────────────────────────

// ThrottledSignaler wraps a RiskAnalysisSignaler with per-project throttling.
// The first signal fires immediately. Subsequent signals within the cooldown
// are coalesced into a single trailing signal when the window expires.
type ThrottledSignaler struct {
	inner    RiskAnalysisSignaler
	logger   *slog.Logger
	throttle *throttle.Throttle[uuid.UUID, uuid.UUID]
}

// NewThrottledSignaler wraps inner with a per-project cooldown. A zero or
// negative cooldown disables throttling.
func NewThrottledSignaler(inner RiskAnalysisSignaler, cooldown time.Duration, logger *slog.Logger) *ThrottledSignaler {
	ts := &ThrottledSignaler{
		inner:    inner,
		logger:   logger,
		throttle: nil,
	}
	ts.throttle = throttle.New(cooldown, func(projectID uuid.UUID) uuid.UUID {
		return projectID
	}, func(projectID uuid.UUID) error {
		if err := inner.Signal(context.Background(), projectID); err != nil {
			logger.ErrorContext(context.Background(), "throttled trailing risk signal failed",
				attr.SlogError(err),
				attr.SlogProjectID(projectID.String()),
			)
			return fmt.Errorf("throttled trailing signal: %w", err)
		}
		logger.DebugContext(context.Background(), "risk signal fired (trailing edge)",
			attr.SlogProjectID(projectID.String()),
		)
		return nil
	})
	return ts
}

func (t *ThrottledSignaler) Signal(ctx context.Context, projectID uuid.UUID) error {
	if t.throttle.Cooldown <= 0 {
		if err := t.inner.Signal(ctx, projectID); err != nil {
			return fmt.Errorf("signal: %w", err)
		}
		return nil
	}
	if t.throttle.Do(projectID) {
		t.logger.DebugContext(ctx, "risk signal fired (leading edge)",
			attr.SlogProjectID(projectID.String()),
		)
		if err := t.inner.Signal(ctx, projectID); err != nil {
			return fmt.Errorf("signal: %w", err)
		}
	} else {
		t.logger.DebugContext(ctx, "risk signal throttled (pending trailing)",
			attr.SlogProjectID(projectID.String()),
		)
	}
	return nil
}

// Shutdown flushes any pending throttled signals. Call during graceful shutdown.
func (t *ThrottledSignaler) Shutdown(_ context.Context) error {
	t.logger.InfoContext(context.Background(), "flushing pending risk analysis signals")
	t.throttle.Flush()
	return nil
}
