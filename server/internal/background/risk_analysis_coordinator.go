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
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

const (
	SignalRiskAnalysisRequested = "risk-analysis-requested"

	// riskAnalysisLookback bounds FetchUnanalyzedMessageIDs to recent messages.
	// Messages older than this are not (re-)analyzed after the initial pass.
	riskAnalysisLookback = 2 * time.Hour

	riskCoordinatorFetchLimit int32 = 20_000
	riskCoordinatorBatchSize  int   = 100

	analyzeBatchStartToCloseTimeout = 50 * time.Minute

	// riskAnalysisRetryBackoff is how long a run that saw a failed batch waits
	// before ContinueAsNew retries the withheld units. Without the self-retry
	// those units would sit unanalyzed until the next organic signal, which is
	// not guaranteed to arrive inside the lookback window.
	riskAnalysisRetryBackoff = 5 * time.Minute
)

// RiskAnalysisCoordinatorParams identifies the project this coordinator runs for.
type RiskAnalysisCoordinatorParams struct {
	ProjectID uuid.UUID
}

// RiskAnalysisCoordinatorWorkflow is a per-project coordinator that:
//  1. Fetches all active policies and unanalyzed message IDs (within lookback).
//  2. Fans out AnalyzeBatch activities across all policy×batch combinations.
//  3. Fans in, then marks as analyzed only the units whose every covering
//     activity succeeded. A run that saw failures backs off and retries via
//     ContinueAsNew until the withheld units succeed or age out of lookback.
//
// It sleeps until signaled (SignalRiskAnalysisRequested) and uses
// ContinueAsNew to keep history bounded. Backfills and policy-version
// rescans are handled out-of-band; this workflow only covers units within
// the lookback window.
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

	idLowerBound := uuidv7.LowerBound(workflow.Now(ctx).Add(-riskAnalysisLookback))

	var a *Activities
	var fetchResult risk_analysis.FetchUnanalyzedResult
	if err := workflow.ExecuteActivity(ctx, a.FetchUnanalyzedMessages, risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    params.ProjectID,
		IDLowerBound: idLowerBound,
		BatchLimit:   riskCoordinatorFetchLimit,
	}).Get(ctx, &fetchResult); err != nil {
		logger.Error("fetch unanalyzed messages failed", "error", err.Error())
	}

	if (len(fetchResult.MessageIDs) > 0 || len(fetchResult.ContentPartIDs) > 0) && len(fetchResult.Policies) > 0 {
		// Fan-out: one activity per (policy, batch). Each dispatch remembers the
		// unit ids its activity covers so the fan-in can tell which units were
		// durably analyzed.
		type analyzeDispatch struct {
			future         workflow.Future
			riskPolicyID   uuid.UUID
			messageIDs     []uuid.UUID
			contentPartIDs []uuid.UUID
		}
		var dispatches []analyzeDispatch
		for _, policy := range fetchResult.Policies {
			for _, batch := range chunkUUIDs(fetchResult.MessageIDs, riskCoordinatorBatchSize) {
				f := workflow.ExecuteActivity(analyzeBatchCtx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
					ProjectID:        params.ProjectID,
					OrganizationID:   policy.OrganizationID,
					RiskPolicyID:     policy.ID,
					PolicyVersion:    policy.Version,
					MessageIDs:       batch,
					ContentPartIDs:   nil,
					Sources:          policy.Sources,
					MessageTypes:     policy.MessageTypes,
					PresidioEntities: policy.PresidioEntities,
					// Derived authoritatively from the policy inside AnalyzeBatch.Do;
					// left unset here to avoid a second config source.
					PresidioScoreThreshold: 0,
					ApprovedEmailDomains:   nil,
					CustomRuleIds:          policy.CustomRuleIds,
					// Placeholder: overwritten by AnalyzeBatch.Do from the refetched
					// policy's AnalyzerConfig (defaults to true). The zero value here
					// does NOT disable the library; it is required only by exhaustruct.
					BuiltinPresetsEnabled: false,
					DetectionScopes:       nil,
				})
				dispatches = append(dispatches, analyzeDispatch{future: f, riskPolicyID: policy.ID, messageIDs: batch, contentPartIDs: nil})
			}
			for _, batch := range chunkUUIDs(fetchResult.ContentPartIDs, riskCoordinatorBatchSize) {
				f := workflow.ExecuteActivity(analyzeBatchCtx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
					ProjectID:              params.ProjectID,
					OrganizationID:         policy.OrganizationID,
					RiskPolicyID:           policy.ID,
					PolicyVersion:          policy.Version,
					MessageIDs:             nil,
					ContentPartIDs:         batch,
					Sources:                policy.Sources,
					MessageTypes:           policy.MessageTypes,
					PresidioEntities:       policy.PresidioEntities,
					PresidioScoreThreshold: 0,
					ApprovedEmailDomains:   nil,
					CustomRuleIds:          policy.CustomRuleIds,
					BuiltinPresetsEnabled:  false,
					DetectionScopes:        nil,
				})
				dispatches = append(dispatches, analyzeDispatch{future: f, riskPolicyID: policy.ID, messageIDs: nil, contentPartIDs: batch})
			}
		}

		// Fan-in: log failures and withhold the failed batches' units from the
		// analyzed mark. An exhausted retry means the batch's findings may never
		// have been durably written, so marking would drop those units
		// permanently; left unmarked they are refetched by the next cycle's
		// lookback instead. A unit is marked only when every policy's activity
		// covering it succeeded. Accepted cost: a persistently failing batch is
		// re-analyzed each cycle until its units age out of the lookback, and
		// units that age out stay unmarked. The failed sets are membership
		// lookups only, never iterated, so workflow determinism holds.
		failedMessages := make(map[uuid.UUID]struct{})
		failedParts := make(map[uuid.UUID]struct{})
		for _, d := range dispatches {
			if err := d.future.Get(ctx, nil); err != nil {
				logger.Error("analyze batch failed",
					"error", err.Error(),
					"risk_policy_id", d.riskPolicyID.String(),
					"message_count", len(d.messageIDs),
					"content_part_count", len(d.contentPartIDs),
				)
				for _, id := range d.messageIDs {
					failedMessages[id] = struct{}{}
				}
				for _, id := range d.contentPartIDs {
					failedParts[id] = struct{}{}
				}
			}
		}

		markMessages := excludeUUIDs(fetchResult.MessageIDs, failedMessages)
		markParts := excludeUUIDs(fetchResult.ContentPartIDs, failedParts)
		markFailed := false
		if len(markMessages) > 0 || len(markParts) > 0 {
			if err := workflow.ExecuteActivity(ctx, a.MarkMessagesAnalyzed, risk_analysis.MarkMessagesAnalyzedArgs{
				ProjectID:      params.ProjectID,
				MessageIDs:     markMessages,
				ContentPartIDs: markParts,
			}).Get(ctx, nil); err != nil {
				logger.Error("mark messages analyzed failed", "error", err.Error())
				markFailed = true
			}
		}

		// Self-retry: unmarked units are only refetched by a fresh run's
		// lookback query, and no later signal is guaranteed to arrive. Back
		// off, then ContinueAsNew to retry them. A failed mark leaves its
		// units unmarked exactly like a failed batch, so it retries the same
		// way. The loop ends on its own — either a retry succeeds, or the
		// failing units age out of the lookback and the next run's fetch
		// dispatches nothing.
		if markFailed || len(failedMessages) > 0 || len(failedParts) > 0 {
			if err := workflow.Sleep(ctx, riskAnalysisRetryBackoff); err != nil {
				return fmt.Errorf("retry backoff: %w", err)
			}
			drainSignals(signalCh)
			return workflow.NewContinueAsNewError(ctx, RiskAnalysisCoordinatorWorkflow, params)
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

// excludeUUIDs returns ids minus those present in drop, preserving order.
func excludeUUIDs(ids []uuid.UUID, drop map[uuid.UUID]struct{}) []uuid.UUID {
	if len(drop) == 0 {
		return ids
	}
	kept := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := drop[id]; !ok {
			kept = append(kept, id)
		}
	}
	return kept
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

// TemporalRiskAnalysisSignaler signals the per-project risk analysis
// coordinator workflow over Temporal. Consumers declare the narrow interface
// they need — risk.RiskAnalysisSignaler, ProjectSignaler below.
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

// ProjectSignaler wakes a per-project coordinator workflow. Every coordinator
// in this package is signalled the same way — a project id, no payload, one
// live run per project — so one throttle serves all of them; the logger a
// caller hands NewThrottledSignaler is what names which one in the logs.
type ProjectSignaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
}

// ThrottledSignaler wraps a ProjectSignaler with per-project throttling.
// The first signal fires immediately. Subsequent signals within the cooldown
// are coalesced into a single trailing signal when the window expires.
type ThrottledSignaler struct {
	inner    ProjectSignaler
	logger   *slog.Logger
	throttle *throttle.Throttle[uuid.UUID, uuid.UUID]
}

// NewThrottledSignaler wraps inner with a per-project cooldown. A zero or
// negative cooldown disables throttling.
func NewThrottledSignaler(inner ProjectSignaler, cooldown time.Duration, logger *slog.Logger) *ThrottledSignaler {
	ts := &ThrottledSignaler{
		inner:    inner,
		logger:   logger,
		throttle: nil,
	}
	ts.throttle = throttle.New(cooldown, func(projectID uuid.UUID) uuid.UUID {
		return projectID
	}, func(projectID uuid.UUID) error {
		if err := inner.Signal(context.Background(), projectID); err != nil {
			logger.ErrorContext(context.Background(), "throttled trailing coordinator signal failed",
				attr.SlogError(err),
				attr.SlogProjectID(projectID.String()),
			)
			return fmt.Errorf("throttled trailing signal: %w", err)
		}
		logger.DebugContext(context.Background(), "coordinator signal fired (trailing edge)",
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
		t.logger.DebugContext(ctx, "coordinator signal fired (leading edge)",
			attr.SlogProjectID(projectID.String()),
		)
		if err := t.inner.Signal(ctx, projectID); err != nil {
			return fmt.Errorf("signal: %w", err)
		}
	} else {
		t.logger.DebugContext(ctx, "coordinator signal throttled (pending trailing)",
			attr.SlogProjectID(projectID.String()),
		)
	}
	return nil
}

// Shutdown flushes any pending throttled signals. Call during graceful shutdown.
func (t *ThrottledSignaler) Shutdown(_ context.Context) error {
	t.logger.InfoContext(context.Background(), "flushing pending coordinator signals")
	t.throttle.Flush()
	return nil
}
