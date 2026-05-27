package background

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestAnalyzeNewMessageWorkflowID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wfID := analyzeNewMessageWorkflowID(id)
	require.Equal(t, "v1:analyze-new-message:550e8400-e29b-41d4-a716-446655440000", wfID)
}

func TestAnalyzeNewMessageWorkflow_DrainsBatch(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	policyID := uuid.New()

	msgs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	var (
		mu              sync.Mutex
		metaCallCount   int
		analyzedBatches [][]uuid.UUID
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			mu.Lock()
			defer mu.Unlock()
			metaCallCount++
			require.Equal(t, projectID, args.ProjectID)
			require.Equal(t, policyID, args.RiskPolicyID)
			return &risk_analysis.GetRiskPolicyMetadataResult{
				Enabled:        true,
				OrganizationID: "org-1",
				PolicyVersion:  7,
			}, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, projectID, args.ProjectID)
			require.Equal(t, policyID, args.RiskPolicyID)
			require.Equal(t, int64(7), args.PolicyVersion)
			batch := append([]uuid.UUID(nil), args.MessageIDs...)
			analyzedBatches = append(analyzedBatches, batch)
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs)}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		for _, id := range msgs {
			env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: id})
		}
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    projectID,
		RiskPolicyID: policyID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, metaCallCount)
	require.Len(t, analyzedBatches, 1)
	require.ElementsMatch(t, msgs, analyzedBatches[0])
}

func TestAnalyzeNewMessageWorkflow_NoSignalsExits(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			t.Fatal("GetRiskPolicyMetadata should not be called with no signals")
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			t.Fatal("AnalyzeBatch should not be called with no signals")
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

func TestAnalyzeNewMessageWorkflow_DisabledPolicyShortCircuits(t *testing.T) {
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
			t.Fatal("AnalyzeBatch should not be called when policy is disabled")
			return nil, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: uuid.New()})
		// A signal that arrives after the disabled check should be drained
		// so the next SignalWithStart starts a clean run.
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: uuid.New()})
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestAnalyzeNewMessageWorkflow_MidCycleSignalCausesSecondLoop(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	firstBatchMsg := uuid.New()
	secondBatchMsg := uuid.New()

	var (
		mu              sync.Mutex
		analyzedBatches [][]uuid.UUID
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			return &risk_analysis.GetRiskPolicyMetadataResult{Enabled: true, PolicyVersion: 1}, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			mu.Lock()
			batchIdx := len(analyzedBatches)
			analyzedBatches = append(analyzedBatches, append([]uuid.UUID(nil), args.MessageIDs...))
			mu.Unlock()
			// Inject a fresh signal during the first analyze call to force
			// the workflow into a second drain pass.
			if batchIdx == 0 {
				env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: secondBatchMsg})
			}
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs)}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: firstBatchMsg})
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, analyzedBatches, 2)
	require.ElementsMatch(t, []uuid.UUID{firstBatchMsg}, analyzedBatches[0])
	require.ElementsMatch(t, []uuid.UUID{secondBatchMsg}, analyzedBatches[1])
}

func TestAnalyzeNewMessageWorkflow_NilUUIDSkipped(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	realMsg := uuid.New()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.GetRiskPolicyMetadataArgs) (*risk_analysis.GetRiskPolicyMetadataResult, error) {
			return &risk_analysis.GetRiskPolicyMetadataResult{Enabled: true}, nil
		},
		activity.RegisterOptions{Name: "GetRiskPolicyMetadata"},
	)

	var analyzedBatch []uuid.UUID
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			analyzedBatch = append([]uuid.UUID(nil), args.MessageIDs...)
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs)}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: uuid.Nil})
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: realMsg})
		env.SignalWorkflow(SignalAnalyzeMessageRequested, AnalyzeMessageSignalPayload{MessageID: uuid.Nil})
	}, 0)

	env.ExecuteWorkflow(AnalyzeNewMessageWorkflow, AnalyzeNewMessageParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []uuid.UUID{realMsg}, analyzedBatch)
}
