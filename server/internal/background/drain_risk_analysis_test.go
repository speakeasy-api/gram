package background

import (
	"context"
	"testing"

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

func TestDrainWorkflow_EmptyDrainWaitsForSignal(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Always returns empty.
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

	// Workflow drains (nothing to do), then blocks on signalCh.Receive().
	// Send a signal so it ContinueAsNew.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRiskAnalysisRequested, nil)
	}, 0)

	env.ExecuteWorkflow(DrainRiskAnalysisWorkflow, DrainRiskAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	// Workflow ContinueAsNew after receiving the signal.
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)
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
