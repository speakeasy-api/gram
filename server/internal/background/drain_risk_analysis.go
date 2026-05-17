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

	// DefaultRecentMessagesBudget caps the per-cycle drain triggered by
	// new-message ingest and policy edits to the most recent N
	// unanalyzed messages. Explicit user backfill (TriggerRiskAnalysis)
	// can override this with a higher cap or 0 (unbounded).
	DefaultRecentMessagesBudget int32 = 100

	// drainBatchSize is how many messages each AnalyzeBatch activity
	// processes. Sized 2026-05-13 alongside analyzeBatchStartToCloseTimeout:
	// Presidio is serialized (presidioMaxMessageBytes = byte semaphore
	// budget) at up to ~30 s per message under load, so 100 messages caps
	// the worst-case happy-path activity wall-clock at ~50 minutes.
	drainBatchSize = 100

	// Tuned 2026-05-01. Fleet-wide cap is perPodAnalyzeBatchConcurrency.
	perDrainBatchConcurrency = 1

	// analyzeBatchStartToCloseTimeout caps how long a single AnalyzeBatch
	// activity may run before Temporal cancels it. Derived from the
	// drainBatchSize × per-message worst-case happy-path latency budget
	// (100 × ~30 s ≈ 50 min). Cancellation is a hard kill: any in-flight
	// presidio retries lose their budget, and Temporal will retry the
	// whole activity per RetryPolicy below.
	analyzeBatchStartToCloseTimeout = 50 * time.Minute
)

// DrainRiskAnalysisParams identifies the policy this workflow drains.
// Version and sources are read from the DB on each drain cycle so that
// policy updates are picked up without restarting the workflow.
//
// MaxMessages caps how many unanalyzed messages this workflow processes
// before completing. 0 means unbounded (drain until empty, the legacy
// behavior used by explicit backfill). A positive value caps the run at
// that many messages and skips ContinueAsNew, so new-message and policy
// update signals only trigger a small "recent N" drain by default.
//
// When SignalWithStart finds an existing run, params are ignored; use
// SignalNewMessagesPayload.MaxMessages on the signal itself to deliver
// the budget to the running workflow.
type DrainRiskAnalysisParams struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
	MaxMessages  int32
}

// SignalNewMessagesPayload is the payload delivered with
// SignalRiskAnalysisRequested. It lets concurrent signal sites bump the
// per-cycle budget of an already-running workflow. The workflow takes
// the most-permissive value across all pending signals (0 = unbounded
// wins over any positive cap).
type SignalNewMessagesPayload struct {
	MaxMessages int32
}

// DrainRiskAnalysisWorkflow is a perpetual "one-man queue" for a single risk
// policy. It drains all unanalyzed messages, then sleeps until signaled.
// ContinueAsNew keeps history bounded.
func DrainRiskAnalysisWorkflow(ctx workflow.Context, params DrainRiskAnalysisParams) error {
	logger := workflow.GetLogger(ctx)
	signalCh := workflow.GetSignalChannel(ctx, SignalRiskAnalysisRequested)

	// budget caps how many messages this run will fetch and process.
	// 0 means unbounded (drain to empty). Signals that arrive during
	// the drain are folded into the next cycle's budget below; we don't
	// rescale mid-fetch.
	budget := params.MaxMessages

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
	// StartToCloseTimeout overrides activityOpts (5m). Presidio calls
	// serialize at up to ~30 s per message, so a drainBatchSize batch can
	// take up to ~50 minutes in the worst case. See analyzeBatchStartToCloseTimeout
	// for the derivation; keep the constants moving together.
	analyzeBatchOpts.StartToCloseTimeout = analyzeBatchStartToCloseTimeout
	// HeartbeatTimeout overrides activityOpts (30s). onProgress() (which
	// emits the heartbeat) fires before each /analyze call and again
	// while blocked on the byte-throttle semaphore; the per-request
	// timeout is analyzeRequestTimeout = 30 s. 60 s gives a 2x buffer over
	// that ceiling so a single stalled call cannot starve the heartbeat.
	analyzeBatchOpts.HeartbeatTimeout = 60 * time.Second
	analyzeBatchCtx := workflow.WithActivityOptions(ctx, analyzeBatchOpts)

	var a *Activities

	// ── Fetch unanalyzed messages ──────────────────────────────────────
	// Reads the current policy version from the DB each time, so version
	// bumps (policy updates) are picked up automatically.
	fetchLimit := drainFetchLimit
	if budget > 0 && budget < fetchLimit {
		fetchLimit = budget
	}
	var fetchResult risk_analysis.FetchUnanalyzedResult
	err := workflow.ExecuteActivity(ctx, a.FetchUnanalyzedMessages, risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    params.ProjectID,
		RiskPolicyID: params.RiskPolicyID,
		BatchLimit:   fetchLimit,
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
				ProjectID:            params.ProjectID,
				OrganizationID:       fetchResult.OrganizationID,
				RiskPolicyID:         params.RiskPolicyID,
				PolicyVersion:        fetchResult.PolicyVersion,
				MessageIDs:           batch,
				Sources:              fetchResult.Sources,
				PresidioEntities:     fetchResult.PresidioEntities,
				PromptInjectionRules: fetchResult.PromptInjectionRules,
			})
			pending = append(pending, f)
		}

		for _, f := range pending {
			if err := f.Get(ctx, nil); err != nil {
				logger.Error("analyze batch failed", "error", err.Error())
			}
		}

		// Bounded backfill stops here: this run was started with a cap
		// and we already fetched up to that cap. Any signals that
		// arrived during the drain may have requested a more permissive
		// budget — pick it up via ContinueAsNew rather than dropping it.
		signalBudgets, gotAny := drainPendingSignalBudgets(signalCh)
		nextBudget := mergeBackfillBudget(params.MaxMessages, signalBudgets)
		if budget > 0 && !gotAny {
			return nil
		}
		nextParams := params
		nextParams.MaxMessages = nextBudget
		return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, nextParams)
	}

	// ── Complete ───────────────────────────────────────────────────────
	// If signals arrived while we were draining, ContinueAsNew to process them.
	// Otherwise just complete — SignalWithStart will start a new run when needed.
	signalBudgets, gotAny := drainPendingSignalBudgets(signalCh)
	if gotAny {
		nextParams := params
		nextParams.MaxMessages = mergeBackfillBudget(params.MaxMessages, signalBudgets)
		return workflow.NewContinueAsNewError(ctx, DrainRiskAnalysisWorkflow, nextParams)
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

// drainPendingSignalBudgets consumes all queued signal payloads and
// returns the MaxMessages values they carried plus whether at least one
// signal was consumed. Legacy nil payloads decode as zero-valued
// SignalNewMessagesPayload (MaxMessages = 0, i.e. unbounded).
func drainPendingSignalBudgets(ch workflow.ReceiveChannel) ([]int32, bool) {
	var out []int32
	gotAny := false
	for {
		var payload SignalNewMessagesPayload
		if !ch.ReceiveAsync(&payload) {
			return out, gotAny
		}
		gotAny = true
		out = append(out, payload.MaxMessages)
	}
}

// mergeBackfillBudget folds incoming signal budgets into the current
// budget, picking the most-permissive value. 0 (unbounded) beats any
// positive cap; among positive caps the larger wins so a "backfill last
// 1000" arriving during a "last 100" run still gets honored.
func mergeBackfillBudget(current int32, incoming []int32) int32 {
	if current == 0 {
		return 0
	}
	result := current
	for _, v := range incoming {
		if v == 0 {
			return 0
		}
		if v > result {
			result = v
		}
	}
	return result
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
		SignalNewMessagesPayload{MaxMessages: params.MaxMessages},
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
