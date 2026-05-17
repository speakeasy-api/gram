package background

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestDrainWorkflowID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wfID := drainWorkflowID(id)
	assert.Equal(t, "v1:drain-risk-analysis:550e8400-e29b-41d4-a716-446655440000", wfID)
}

func TestDrainWorkflow_DrainsAndSleeps(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	policyID := uuid.New()
	projectID := uuid.New()
	messageIDs := make([]uuid.UUID, 3)
	for i := range messageIDs {
		messageIDs[i] = uuid.New()
	}

	fetchCallCount := 0
	analyzeCallCount := 0

	// First fetch returns messages, second returns empty.
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			fetchCallCount++
			if fetchCallCount == 1 {
				return &risk_analysis.FetchUnanalyzedResult{MessageIDs: messageIDs}, nil
			}
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			analyzeCallCount++
			require.Equal(t, policyID, args.RiskPolicyID)
			require.Equal(t, projectID, args.ProjectID)
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs), Findings: 0}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	// After draining, the workflow sleeps. Send a signal after a delay so it
	// doesn't block forever — the idle timeout will complete it.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRiskAnalysisRequested, nil)
	}, 0)

	env.ExecuteWorkflow(DrainRiskAnalysisWorkflow, DrainRiskAnalysisParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
	})

	require.True(t, env.IsWorkflowCompleted())
	// Workflow may ContinueAsNew or complete — both are fine.
	// Verify activities were called.
	require.GreaterOrEqual(t, fetchCallCount, 1) // at least one fetch
	require.Equal(t, 1, analyzeCallCount)
}

func TestDrainWorkflow_SignalDuringDrainContinuesAsNew(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Fetch is async: it lets a signal land before returning. This
	// models a real signal arriving mid-cycle (e.g. a backfill click
	// while ingest-triggered drain is in flight). The workflow should
	// pick up that signal at the end-of-cycle drain and ContinueAsNew.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			t.Fatal("AnalyzeBatch should not be called when there are no messages")
			return nil, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	// Delay > 0 so the signal arrives after the workflow's start-time
	// signal drain. Otherwise it would be absorbed at the top and the
	// run would simply complete.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRiskAnalysisRequested, SignalNewMessagesPayload{MaxMessages: 0})
	}, time.Millisecond)

	env.ExecuteWorkflow(DrainRiskAnalysisWorkflow, DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)
}

func TestDrainWorkflow_StartSignalDoesNotSelfLoop(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// SignalWithStart leaves the triggering signal in the channel. The
	// workflow must absorb it at the top of the run; otherwise the
	// end-of-cycle drain sees it as "new signal arrived" and
	// ContinueAsNews forever with the same bounded budget.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	// Simulate the SignalWithStart-delivered signal: queued at virtual
	// time 0, before any workflow code runs.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRiskAnalysisRequested, SignalNewMessagesPayload{MaxMessages: 100})
	}, 0)

	env.ExecuteWorkflow(DrainRiskAnalysisWorkflow, DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
		MaxMessages:  100,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "expected clean completion, not ContinueAsNew")
}

func TestDrainWorkflow_ActivityFailureContinues(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetchCallCount := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			fetchCallCount++
			if fetchCallCount == 1 {
				return &risk_analysis.FetchUnanalyzedResult{MessageIDs: []uuid.UUID{uuid.New()}}, nil
			}
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			return nil, temporal.NewApplicationError("scan failed", "", nil)
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	// After drain (with failure), workflow blocks on signal. Send one.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRiskAnalysisRequested, nil)
	}, 0)

	env.ExecuteWorkflow(DrainRiskAnalysisWorkflow, DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	// Workflow ContinueAsNew despite the failed batch.
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)
}

func TestChunkUUIDs(t *testing.T) {
	t.Parallel()

	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.New()
	}

	chunks := chunkUUIDs(ids, 2)
	require.Len(t, chunks, 3)
	assert.Len(t, chunks[0], 2)
	assert.Len(t, chunks[1], 2)
	assert.Len(t, chunks[2], 1)

	chunks = chunkUUIDs(nil, 10)
	assert.Empty(t, chunks)
}
