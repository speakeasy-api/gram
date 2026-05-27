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
)

const (
	SignalAnalyzeMessageRequested = "analyze-message-requested"

	analyzeMessageBatchSize = 100
)

// AnalyzeNewMessageParams identifies the policy this workflow analyzes for.
// Message IDs arrive via SignalAnalyzeMessageRequested rather than the
// workflow params, so SignalWithStart can coalesce per-message signals onto
// a single per-policy run.
type AnalyzeNewMessageParams struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
}

// AnalyzeMessageSignalPayload is delivered with SignalAnalyzeMessageRequested.
// Each signal carries exactly one message ID; the workflow drains all queued
// signals into a single batch before invoking AnalyzeBatch.
type AnalyzeMessageSignalPayload struct {
	MessageID uuid.UUID
}

// AnalyzeNewMessageWorkflow is the event-driven counterpart to
// DrainRiskAnalysisWorkflow. The drain workflow exists for backfill triggered
// by policy create/update and explicit user requests; this workflow is
// signaled per newly-captured chat message and never scans the database for
// unanalyzed work. It drains the signal channel into a batch, calls
// AnalyzeBatch on the risk task queue, then loops if more signals arrived
// during processing. With no pending signals at the end of a cycle the
// workflow exits cleanly; the next signal restarts it via SignalWithStart.
func AnalyzeNewMessageWorkflow(ctx workflow.Context, params AnalyzeNewMessageParams) error {
	logger := workflow.GetLogger(ctx)
	signalCh := workflow.GetSignalChannel(ctx, SignalAnalyzeMessageRequested)

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	}
	metaCtx := workflow.WithActivityOptions(ctx, activityOpts)

	analyzeBatchOpts := activityOpts
	analyzeBatchOpts.TaskQueue = RiskAnalysisTaskQueue(tenv.TaskQueueName(workflow.GetInfo(ctx).TaskQueueName))
	analyzeBatchOpts.StartToCloseTimeout = analyzeBatchStartToCloseTimeout
	analyzeBatchOpts.HeartbeatTimeout = 60 * time.Second
	analyzeBatchCtx := workflow.WithActivityOptions(ctx, analyzeBatchOpts)

	var a *Activities

	// Drain the start-of-run signal queue. SignalWithStart leaves the
	// triggering signal in the channel so the first pass collects at
	// least one ID; subsequent passes pick up signals that arrived while
	// AnalyzeBatch was running.
	pending := drainPendingMessageIDs(signalCh)
	for len(pending) > 0 {
		var meta risk_analysis.GetRiskPolicyMetadataResult
		err := workflow.ExecuteActivity(metaCtx, a.GetRiskPolicyMetadata, risk_analysis.GetRiskPolicyMetadataArgs{
			ProjectID:    params.ProjectID,
			RiskPolicyID: params.RiskPolicyID,
		}).Get(ctx, &meta)
		if err != nil {
			logger.Error("get risk policy metadata", "error", err.Error())
			// Drop the in-flight IDs; the policy lookup itself failed so
			// retrying with the same IDs would just fail again. The next
			// signal will start a fresh run that can re-read the policy.
			return nil
		}
		if !meta.Enabled {
			// Drain any signals that piled up while we were checking so
			// the next SignalWithStart starts a clean run.
			drainPendingMessageIDs(signalCh)
			return nil
		}

		for _, batch := range chunkUUIDs(pending, analyzeMessageBatchSize) {
			err := workflow.ExecuteActivity(analyzeBatchCtx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
				ProjectID:            params.ProjectID,
				OrganizationID:       meta.OrganizationID,
				RiskPolicyID:         params.RiskPolicyID,
				PolicyVersion:        meta.PolicyVersion,
				MessageIDs:           batch,
				Sources:              meta.Sources,
				PresidioEntities:     meta.PresidioEntities,
				PromptInjectionRules: meta.PromptInjectionRules,
			}).Get(ctx, nil)
			if err != nil {
				logger.Error("analyze batch failed", "error", err.Error())
			}
		}

		pending = drainPendingMessageIDs(signalCh)
	}

	return nil
}

// drainPendingMessageIDs consumes all queued AnalyzeMessageSignalPayload
// signals and returns the message IDs they carried. Nil-UUID payloads (an
// uninitialized struct) are skipped so a malformed signal cannot poison the
// batch.
func drainPendingMessageIDs(ch workflow.ReceiveChannel) []uuid.UUID {
	var out []uuid.UUID
	for {
		var payload AnalyzeMessageSignalPayload
		if !ch.ReceiveAsync(&payload) {
			return out
		}
		if payload.MessageID == uuid.Nil {
			continue
		}
		out = append(out, payload.MessageID)
	}
}

// AnalyzeNewMessageSignaler delivers per-message signals to the analyze
// workflow. There is intentionally no throttling wrapper: every message must
// reach the workflow or it will silently miss analysis.
type AnalyzeNewMessageSignaler interface {
	SignalNewMessage(ctx context.Context, params AnalyzeNewMessageParams, messageID uuid.UUID) error
}

type TemporalAnalyzeNewMessageSignaler struct {
	TemporalEnv *tenv.Environment
	Logger      *slog.Logger
}

func (s *TemporalAnalyzeNewMessageSignaler) SignalNewMessage(ctx context.Context, params AnalyzeNewMessageParams, messageID uuid.UUID) error {
	if messageID == uuid.Nil {
		return fmt.Errorf("signal analyze workflow: message ID is nil")
	}

	wfID := analyzeNewMessageWorkflowID(params.RiskPolicyID)

	_, err := s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		wfID,
		SignalAnalyzeMessageRequested,
		AnalyzeMessageSignalPayload{MessageID: messageID},
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		},
		AnalyzeNewMessageWorkflow,
		params,
	)
	if err != nil {
		return fmt.Errorf("signal-with-start analyze workflow: %w", err)
	}

	s.Logger.DebugContext(ctx, "temporal signal-with-start sent for analyze workflow",
		attr.SlogRiskPolicyID(params.RiskPolicyID.String()),
		attr.SlogTemporalWorkflowID(wfID),
	)
	return nil
}

func analyzeNewMessageWorkflowID(policyID uuid.UUID) string {
	return fmt.Sprintf("v1:analyze-new-message:%s", policyID.String())
}
