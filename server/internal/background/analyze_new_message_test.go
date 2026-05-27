package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestAnalyzeNewMessageWorkflowID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wfID := analyzeNewMessageWorkflowID(id)
	assert.Equal(t, "v1:analyze-new-message:550e8400-e29b-41d4-a716-446655440000", wfID)
}

func TestAnalyzeNewMessageWorkflow_DrainsSignalsAndBatches(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	policyID := uuid.New()
	projectID := uuid.New()
	msgIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	metaCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			metaCallCount++
			return &risk_analysis.GetRiskPolicyMetadataResult{Enabled: true, PolicyVersion: 1}, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)

	var analyzed []uuid.UUID
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			require.Equal(t, policyID, args.RiskPolicyID)
			require.Equal(t, projectID, args.ProjectID)
			analyzed = append(analyzed, args.MessageIDs...)
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs)}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	// Pre-queue all signals at virtual time 0 so the workflow drains them
	// all in the first pass, exactly as SignalWithStart would deliver them
	// to a brand-new run.
	env.RegisterDelayedCallback(func() {
		for _, id := range msgIDs {
			env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: id})
		}
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, metaCallCount, "metadata fetched once per cycle")
	require.ElementsMatch(t, msgIDs, analyzed)
}

func TestAnalyzeNewMessageWorkflow_NoSignalsExitsCleanly(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			t.Fatal("metadata activity should not be called when there are no signals")
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			t.Fatal("analyze batch should not be called when there are no signals")
			return nil, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestAnalyzeNewMessageWorkflow_DisabledPolicyDropsSignals(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			return &risk_analysis.GetRiskPolicyMetadataResult{Enabled: false}, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			t.Fatal("analyze batch must not be called when policy is disabled")
			return nil, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: uuid.New()})
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestAnalyzeNewMessageWorkflow_SignalsDuringBatchLoop(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	policyID := uuid.New()
	projectID := uuid.New()
	firstID := uuid.New()
	midID := uuid.New()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			return &risk_analysis.GetRiskPolicyMetadataResult{Enabled: true, PolicyVersion: 1}, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)

	var batches [][]uuid.UUID
	sentMid := false
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			batches = append(batches, append([]uuid.UUID(nil), args.MessageIDs...))
			if !sentMid {
				// Mid-cycle signal: the workflow must observe it on the
				// end-of-loop drain and run a second analyze pass.
				env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: midID})
				sentMid = true
			}
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs)}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: firstID})
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, batches, 2)
	require.Equal(t, []uuid.UUID{firstID}, batches[0])
	require.Equal(t, []uuid.UUID{midID}, batches[1])
}

func TestAnalyzeNewMessageWorkflow_SkipsNilUUIDPayloads(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			t.Fatal("metadata activity should not run when every signal is a nil UUID")
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			t.Fatal("analyze batch must not be invoked")
			return nil, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: uuid.Nil})
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
