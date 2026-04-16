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
	// SignalNewMessages is sent to wake the drain workflow when new chat messages
	// arrive for a project that has enabled risk policies.
	SignalNewMessages = "new-messages"

	// drainBatchSize is the number of unanalyzed messages to fetch per activity call.
	drainBatchSize int32 = 100

	// drainSleepBetweenBatches prevents tight-looping while still draining quickly.
	drainSleepBetweenBatches = 2 * time.Second

	// drainIdleTimeout is how long the workflow waits for new signals before
	// completing (to be restarted on the next signal).
	drainIdleTimeout = 10 * time.Minute
)

// DrainRiskAnalysisParams are the inputs to the drain workflow.
type DrainRiskAnalysisParams struct {
	ProjectID     uuid.UUID
	RiskPolicyID  uuid.UUID
	PolicyVersion int64
	Sources       []string
}

// TemporalRiskAnalysisSignaler signals or starts the drain workflow for a
// project's risk policies. It is called from the chat service when new messages
// are stored.
type TemporalRiskAnalysisSignaler struct {
	TemporalEnv *tenv.Environment
}

// SignalNewMessages signals the drain workflow for the given policy. If the
// workflow does not exist yet, it starts a new one.
func (s *TemporalRiskAnalysisSignaler) SignalNewMessages(ctx context.Context, params DrainRiskAnalysisParams) error {
	if s.TemporalEnv == nil {
		return nil
	}

	wfID := drainWorkflowID(params.RiskPolicyID)

	// Try to signal an already-running workflow.
	err := s.TemporalEnv.Client().SignalWorkflow(ctx, wfID, "", SignalNewMessages, nil)
	if err == nil {
		return nil
	}

	// Workflow not running - start it with signal-with-start.
	_, err = s.TemporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		wfID,
		SignalNewMessages,
		nil, // signal arg
		client.StartWorkflowOptions{
			ID:                    wfID,
			TaskQueue:             string(s.TemporalEnv.Queue()),
			WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowRunTimeout:    1 * time.Hour,
		},
		DrainRiskAnalysisWorkflow,
		params,
	)
	if err != nil {
		return fmt.Errorf("signal-with-start drain risk analysis workflow: %w", err)
	}
	return nil
}

func ExecuteDrainRiskAnalysisWorkflow(ctx context.Context, env *tenv.Environment, params DrainRiskAnalysisParams) (client.WorkflowRun, error) {
	id := drainWorkflowID(params.RiskPolicyID)
	return env.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(env.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    1 * time.Hour,
	}, DrainRiskAnalysisWorkflow, params)
}

// DrainRiskAnalysisWorkflow is a Temporal workflow that continuously fetches
// unanalyzed messages for a risk policy and scans them in batches.
//
// The workflow sleeps until signalled (via SignalNewMessages) or until the idle
// timeout elapses, at which point it completes. A new signal-with-start call
// will restart it when needed.
func DrainRiskAnalysisWorkflow(ctx workflow.Context, params DrainRiskAnalysisParams) error {
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 90 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    5,
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	var a *Activities
	signalCh := workflow.GetSignalChannel(ctx, SignalNewMessages)

	for {
		// Drain all currently unanalyzed messages.
		drained, err := drainAllBatches(ctx, a, params)
		if err != nil {
			return fmt.Errorf("drain batches: %w", err)
		}

		if drained == 0 {
			// Nothing to do - wait for a signal or timeout.
			timerCtx, cancelTimer := workflow.WithCancel(ctx)
			timer := workflow.NewTimer(timerCtx, drainIdleTimeout)

			selector := workflow.NewSelector(ctx)
			gotSignal := false

			selector.AddReceive(signalCh, func(c workflow.ReceiveChannel, _ bool) {
				var v any
				c.Receive(ctx, &v)
				gotSignal = true
				cancelTimer()
			})
			selector.AddFuture(timer, func(f workflow.Future) {
				// Timer expired.
			})

			selector.Select(ctx)

			if !gotSignal {
				// Idle timeout - workflow completes.
				return nil
			}
			// Got a signal - loop back to drain.
			continue
		}

		// Drain any pending signals so we don't immediately re-enter.
		for signalCh.ReceiveAsync(nil) {
		}
	}
}

// drainAllBatches fetches and analyzes batches until no more unanalyzed
// messages remain for the policy version. Returns the total number of messages
// processed.
func drainAllBatches(ctx workflow.Context, a *Activities, params DrainRiskAnalysisParams) (int, error) {
	total := 0
	for {
		var fetchResult risk_analysis.FetchUnanalyzedResult
		err := workflow.ExecuteActivity(ctx, a.FetchUnanalyzedMessages, risk_analysis.FetchUnanalyzedArgs{
			ProjectID:     params.ProjectID,
			RiskPolicyID:  params.RiskPolicyID,
			PolicyVersion: params.PolicyVersion,
			BatchLimit:    drainBatchSize,
		}).Get(ctx, &fetchResult)
		if err != nil {
			return total, fmt.Errorf("fetch unanalyzed: %w", err)
		}

		if len(fetchResult.MessageIDs) == 0 {
			break
		}

		var analyzeResult risk_analysis.AnalyzeBatchResult
		err = workflow.ExecuteActivity(ctx, a.AnalyzeBatch, risk_analysis.AnalyzeBatchArgs{
			ProjectID:     params.ProjectID,
			RiskPolicyID:  params.RiskPolicyID,
			PolicyVersion: params.PolicyVersion,
			MessageIDs:    fetchResult.MessageIDs,
			Sources:       params.Sources,
		}).Get(ctx, &analyzeResult)
		if err != nil {
			return total, fmt.Errorf("analyze batch: %w", err)
		}

		total += analyzeResult.Processed

		// If we got fewer than the batch size, we're done.
		if len(fetchResult.MessageIDs) < int(drainBatchSize) {
			break
		}

		// Small sleep between batches to avoid overwhelming the DB.
		_ = workflow.Sleep(ctx, drainSleepBetweenBatches)
	}
	return total, nil
}

func drainWorkflowID(policyID uuid.UUID) string {
	return fmt.Sprintf("v1:drain-risk-analysis:%s", policyID.String())
}
