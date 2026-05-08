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
	// SignalRiskAnalysisRequested wakes the drain workflow on new messages or
	// policy updates.
	SignalRiskAnalysisRequested = "risk-analysis-requested"

	// drainFetchLimit is how many unanalyzed message IDs to fetch per round.
	drainFetchLimit int32 = 20_000

	// drainBatchSize is how many messages each AnalyzeBatch activity processes.
	drainBatchSize = 1_000

	// Tuned 2026-05-01. Fleet-wide cap is perPodAnalyzeBatchConcurrency.
	perDrainBatchConcurrency = 1
)

// DrainRiskAnalysisParams identifies the policy this workflow drains.
// Version and sources are read from the DB on each drain cycle so that
// policy updates are picked up without restarting the workflow.
type DrainRiskAnalysisParams struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
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

	// AnalyzeBatch runs on a dedicated, capped queue derived from the
	// workflow's own queue so each environment stays isolated.
	analyzeBatchOpts := activityOpts
	analyzeBatchOpts.TaskQueue = RiskAnalysisTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName))
	analyzeBatchCtx := workflow.WithActivityOptions(ctx, analyzeBatchOpts)

	var a *Activities

	// ── Fetch unanalyzed messages ──────────────────────────────────────
	// Reads the current policy version from the DB each time, so version
	// bumps (policy updates) are picked up automatically.
	var fetchResult risk_analysis.FetchUnanalyzedResult
	err := workflow.ExecuteActivity(ctx, a.FetchUnanalyzedMessages, risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    params.ProjectID,
		RiskPolicyID: params.RiskPolicyID,
		BatchLimit:   drainFetchLimit,
	}).Get(ctx, &fetchResult)
	if err != nil {
		logger.Error("fetch unanalyzed message IDs", "error", err.Error())
		// Fall through to sleep — the next signal will retry.
	}

	// ── Analyze batches ────────────────────────────────────────────────
	if len(fetchResult.MessageIDs) > 0 {
		batches := chunkUUIDs(fetchResult.MessageIDs, drainBatchSize)
		pending := make([]workflow.Future, 0, min(len(batches), perDrainBatchConcurrency))

		for _, batch := range batches {
			if len(pending) >= perDrainBatchConcurrency {
				if err := pending[0].Get(ctx, nil); err != nil {
					logger.Error("analyze batch failed", "error", err.Error())
				}
				pending = pending[1:]
			}

			f := workflow.ExecuteActivity(analyzeBatchCtx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
				ProjectID:        params.ProjectID,
				OrganizationID:   fetchResult.OrganizationID,
				RiskPolicyID:     params.RiskPolicyID,
				PolicyVersion:    fetchResult.PolicyVersion,
				MessageIDs:       batch,
				Sources:          fetchResult.Sources,
				PresidioEntities: fetchResult.PresidioEntities,
			})
			pending = append(pending, f)
		}

		for _, f := range pending {
			if err := f.Get(ctx, nil); err != nil {
				logger.Error("analyze batch failed", "error", err.Error())
			}
		}

		// More messages may remain — ContinueAsNew to process them.
		drainSignals(signalCh)
		return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, params)
	}

	// ── Complete ───────────────────────────────────────────────────────
	// If signals arrived while we were draining, ContinueAsNew to process them.
	// Otherwise just complete — SignalWithStart will start a new run when needed.
	if drainSignals(signalCh) {
		return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, params)
	}

	return nil
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
}

// TemporalRiskAnalysisSignaler implements RiskAnalysisSignaler using Temporal.
type TemporalRiskAnalysisSignaler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

func (s *TemporalRiskAnalysisSignaler) SignalNewMessages(ctx context.Context, params DrainRiskAnalysisParams) error {
	wfID := drainWorkflowID(params.RiskPolicyID)

	// SignalWithStartWorkflow atomically signals an existing workflow or
	// starts a new one if none is running. ALLOW_DUPLICATE lets a new run
	// start even after a previous one was terminated or completed.
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
		return fmt.Errorf("signal-with-start drain workflow: %w", err)
	}

	s.Logger.DebugContext(ctx, "temporal signal-with-start sent",
		attr.SlogRiskPolicyID(params.RiskPolicyID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}

func drainWorkflowID(policyID uuid.UUID) string {
	return fmt.Sprintf("v1:drain-risk-analysis:%s", policyID.String())
}

// ── Throttled Signaler ──────────────────────────────────────────────────

// ThrottledSignaler wraps a RiskAnalysisSignaler with per-policy throttling.
// The first signal fires immediately. Subsequent signals within the cooldown
// are coalesced into a single trailing signal when the window expires.
type ThrottledSignaler struct {
	inner    RiskAnalysisSignaler
	logger   *slog.Logger
	throttle *throttle.Throttle[uuid.UUID, DrainRiskAnalysisParams]
}

// NewThrottledSignaler wraps inner with a per-policy cooldown. A zero or
// negative cooldown disables throttling.
func NewThrottledSignaler(inner RiskAnalysisSignaler, cooldown time.Duration, logger *slog.Logger) *ThrottledSignaler {
	ts := &ThrottledSignaler{
		inner:    inner,
		logger:   logger,
		throttle: nil,
	}
	ts.throttle = throttle.New(cooldown, func(params DrainRiskAnalysisParams) uuid.UUID {
		return params.RiskPolicyID
	}, func(params DrainRiskAnalysisParams) error {
		if err := inner.SignalNewMessages(context.Background(), params); err != nil {
			logger.ErrorContext(context.Background(), "throttled trailing signal failed",
				attr.SlogError(err),
				attr.SlogRiskPolicyID(params.RiskPolicyID.String()),
			)
			return fmt.Errorf("throttled trailing signal: %w", err)
		}
		logger.DebugContext(context.Background(), "risk signal fired (trailing edge)",
			attr.SlogRiskPolicyID(params.RiskPolicyID.String()),
		)
		return nil
	})
	return ts
}

func (t *ThrottledSignaler) SignalNewMessages(ctx context.Context, params DrainRiskAnalysisParams) error {
	if t.throttle.Cooldown <= 0 {
		if err := t.inner.SignalNewMessages(ctx, params); err != nil {
			return fmt.Errorf("signal new messages: %w", err)
		}
		return nil
	}
	if t.throttle.Do(params) {
		t.logger.DebugContext(ctx, "risk signal fired (leading edge)",
			attr.SlogRiskPolicyID(params.RiskPolicyID.String()),
		)
		if err := t.inner.SignalNewMessages(ctx, params); err != nil {
			return fmt.Errorf("signal new messages: %w", err)
		}
	} else {
		t.logger.DebugContext(ctx, "risk signal throttled (pending trailing)",
			attr.SlogRiskPolicyID(params.RiskPolicyID.String()),
		)
	}
	return nil
}

// Shutdown flushes any pending throttled signals. Call during graceful shutdown
// to prevent losing trailing signals when a pod restarts.
func (t *ThrottledSignaler) Shutdown(_ context.Context) error {
	t.logger.InfoContext(context.Background(), "flushing pending risk analysis signals")
	t.throttle.Flush()
	return nil
}
