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

func TestCoordinatorWorkflowID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wfID := coordinatorWorkflowID(id)
	assert.Equal(t, "v1:risk-analysis:550e8400-e29b-41d4-a716-446655440000", wfID)
}

func TestCoordinatorWorkflow_FansOutAndMarksAnalyzed(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	policyID := uuid.New()
	messageIDs := make([]uuid.UUID, 3)
	for i := range messageIDs {
		messageIDs[i] = uuid.New()
	}

	analyzeCallCount := 0
	markCallCount := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{
				MessageIDs: messageIDs,
				Policies: []risk_analysis.PolicyForAnalysis{
					{ID: policyID, OrganizationID: "org1", Version: 1},
				},
			}, nil
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

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.MarkMessagesAnalyzedArgs) error {
			markCallCount++
			require.Equal(t, projectID, args.ProjectID)
			require.Len(t, args.MessageIDs, 3)
			return nil
		},
		activity.RegisterOptions{Name: "MarkMessagesAnalyzed"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: projectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, analyzeCallCount)
	require.Equal(t, 1, markCallCount)
}

func TestCoordinatorWorkflow_EmptyFetchCompletesCleanly(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil, Policies: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestCoordinatorWorkflow_SignalDuringProcessingContinuesAsNew(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Signal from inside fetch activity lands mid-cycle; coordinator should ContinueAsNew.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			env.SignalWorkflow(SignalRiskAnalysisRequested, nil)
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil, Policies: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)
}

func TestCoordinatorWorkflow_StartSignalDoesNotSelfLoop(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Simulate SignalWithStart: signal queued before workflow code runs.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalRiskAnalysisRequested, nil)
	}, 0)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{MessageIDs: nil, Policies: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: uuid.New(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "start signal must be drained at top; should complete, not ContinueAsNew")
}

// TestCoordinatorWorkflow_FailedBatchExcludedFromMark pins the at-least-once
// contract on the analyzed mark: a batch whose activity exhausted its retries
// must leave its units unmarked, while units from batches that succeeded are
// still marked — and the run must retry the withheld units via ContinueAsNew
// after the backoff rather than waiting for an organic signal.
func TestCoordinatorWorkflow_FailedBatchExcludedFromMark(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	messageID := uuid.New()
	contentPartID := uuid.New()
	markCallCount := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{
				MessageIDs:     []uuid.UUID{messageID},
				ContentPartIDs: []uuid.UUID{contentPartID},
				Policies:       []risk_analysis.PolicyForAnalysis{{ID: uuid.New(), OrganizationID: "org1", Version: 1}},
			}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	// The message batch fails; the content-part batch succeeds.
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			if len(args.MessageIDs) > 0 {
				return nil, temporal.NewApplicationError("scan failed", "", nil)
			}
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.ContentPartIDs), Findings: 0}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.MarkMessagesAnalyzedArgs) error {
			markCallCount++
			require.Equal(t, projectID, args.ProjectID)
			require.Empty(t, args.MessageIDs, "the failed batch's message must stay unmarked for the next sweep")
			require.Equal(t, []uuid.UUID{contentPartID}, args.ContentPartIDs)
			return nil
		},
		activity.RegisterOptions{Name: "MarkMessagesAnalyzed"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: projectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err, "a run with a failed batch must ContinueAsNew to retry it")
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)
	require.Equal(t, 1, markCallCount, "mark activity still runs for the surviving units")
}

// TestCoordinatorWorkflow_UnitFailedForOnePolicyNotMarked pins the cross-policy
// intersection: the same batch is dispatched once per policy, and a unit is
// only durably analyzed when every policy's activity covering it succeeded.
// When nothing succeeded for every policy, the mark activity is skipped
// entirely and the run retries via ContinueAsNew.
func TestCoordinatorWorkflow_UnitFailedForOnePolicyNotMarked(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	messageID := uuid.New()
	failingPolicyID := uuid.New()
	markCallCount := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{
				MessageIDs: []uuid.UUID{messageID},
				Policies: []risk_analysis.PolicyForAnalysis{
					{ID: uuid.New(), OrganizationID: "org1", Version: 1},
					{ID: failingPolicyID, OrganizationID: "org1", Version: 1},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			if args.RiskPolicyID == failingPolicyID {
				return nil, temporal.NewApplicationError("scan failed", "", nil)
			}
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs), Findings: 0}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.MarkMessagesAnalyzedArgs) error {
			markCallCount++
			return nil
		},
		activity.RegisterOptions{Name: "MarkMessagesAnalyzed"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: projectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err, "a run with a failed batch must ContinueAsNew to retry it")
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr)
	require.Equal(t, 0, markCallCount, "a unit that failed under any policy must not be marked analyzed")
}

// TestCoordinatorWorkflow_MarkFailureRetriesViaContinueAsNew pins that a
// failed MarkMessagesAnalyzed joins the self-retry decision: its units remain
// unmarked exactly like a failed batch's, so the run must ContinueAsNew after
// the backoff instead of completing and waiting for an organic signal.
func TestCoordinatorWorkflow_MarkFailureRetriesViaContinueAsNew(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	messageID := uuid.New()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchUnanalyzedArgs) (*risk_analysis.FetchUnanalyzedResult, error) {
			return &risk_analysis.FetchUnanalyzedResult{
				MessageIDs: []uuid.UUID{messageID},
				Policies:   []risk_analysis.PolicyForAnalysis{{ID: uuid.New(), OrganizationID: "org1", Version: 1}},
			}, nil
		},
		activity.RegisterOptions{Name: "FetchUnanalyzedMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs), Findings: 0}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.MarkMessagesAnalyzedArgs) error {
			return temporal.NewApplicationError("mark failed", "", nil)
		},
		activity.RegisterOptions{Name: "MarkMessagesAnalyzed"},
	)

	env.ExecuteWorkflow(RiskAnalysisCoordinatorWorkflow, RiskAnalysisCoordinatorParams{
		ProjectID: projectID,
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err, "a run whose mark failed must ContinueAsNew to retry it")
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
