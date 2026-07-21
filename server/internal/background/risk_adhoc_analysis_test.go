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

func TestRiskAdhocWorkflowID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	assert.Equal(t, "v1:risk-adhoc:550e8400-e29b-41d4-a716-446655440000", RiskAdhocWorkflowID(id))
}

func TestRiskAdhocWorkflow_FansOutAndReportsProgress(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	policyID := uuid.New()
	messageIDs := make([]uuid.UUID, 250)
	for i := range messageIDs {
		messageIDs[i] = uuid.New()
	}

	countCalls := 0
	analyzeCalls := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.CountAdhocArgs) (int64, error) {
			countCalls++
			require.Equal(t, projectID, args.ProjectID)
			return int64(len(messageIDs)), nil
		},
		activity.RegisterOptions{Name: "CountAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.FetchAdhocArgs) (*risk_analysis.FetchAdhocResult, error) {
			require.Equal(t, projectID, args.ProjectID)
			require.Equal(t, policyID, args.RiskPolicyID.UUID)
			require.Equal(t, uuid.Nil, args.IDCursor)
			return &risk_analysis.FetchAdhocResult{
				MessageIDs: messageIDs,
				Policies: []risk_analysis.PolicyForAnalysis{
					{ID: policyID, OrganizationID: "org1", Version: 3},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "FetchAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			analyzeCalls++
			require.Equal(t, projectID, args.ProjectID)
			require.Equal(t, policyID, args.RiskPolicyID)
			require.Equal(t, int64(3), args.PolicyVersion)
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs), Findings: 1}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.ExecuteWorkflow(RiskAdhocAnalysisWorkflow, RiskAdhocAnalysisParams{
		ProjectID:    projectID,
		RiskPolicyID: uuid.NullUUID{UUID: policyID, Valid: true},
		From:         time.Now().Add(-24 * time.Hour),
		To:           time.Now(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, countCalls)
	require.Equal(t, 3, analyzeCalls, "250 messages at batch size 100 across one policy")

	var progress RiskAdhocProgress
	require.NoError(t, env.GetWorkflowResult(&progress))
	assert.Equal(t, int64(250), progress.TotalMessages)
	assert.Equal(t, int64(250), progress.DispatchedMessages)
	assert.Equal(t, int64(250), progress.ProcessedMessages)
	assert.Equal(t, int64(3), progress.Findings)
	assert.Equal(t, int64(3), progress.BatchesCompleted)
	assert.Equal(t, int64(0), progress.BatchesFailed)
	assert.Equal(t, 1, progress.Policies)
}

func TestRiskAdhocWorkflow_FullPageContinuesAsNew(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	policyID := uuid.New()
	messageIDs := make([]uuid.UUID, riskAdhocFetchPageSize)
	for i := range messageIDs {
		messageIDs[i] = uuid.New()
	}

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.CountAdhocArgs) (int64, error) {
			return int64(len(messageIDs)) * 2, nil
		},
		activity.RegisterOptions{Name: "CountAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchAdhocArgs) (*risk_analysis.FetchAdhocResult, error) {
			return &risk_analysis.FetchAdhocResult{
				MessageIDs: messageIDs,
				Policies:   []risk_analysis.PolicyForAnalysis{{ID: policyID, OrganizationID: "org1", Version: 1}},
			}, nil
		},
		activity.RegisterOptions{Name: "FetchAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs), Findings: 0}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.ExecuteWorkflow(RiskAdhocAnalysisWorkflow, RiskAdhocAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.NullUUID{UUID: policyID, Valid: true},
		From:         time.Now().Add(-24 * time.Hour),
		To:           time.Now(),
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueAsNewErr, "a full fetch page must ContinueAsNew with the cursor advanced")
}

func TestRiskAdhocWorkflow_ResumedRunSkipsCountAndPassesCursor(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	cursor := uuid.New()
	countCalls := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.CountAdhocArgs) (int64, error) {
			countCalls++
			return 0, nil
		},
		activity.RegisterOptions{Name: "CountAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.FetchAdhocArgs) (*risk_analysis.FetchAdhocResult, error) {
			require.Equal(t, cursor, args.IDCursor, "resumed run must fetch from the carried cursor")
			return &risk_analysis.FetchAdhocResult{MessageIDs: nil, Policies: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchAdhocAnalysisMessages"},
	)

	env.ExecuteWorkflow(RiskAdhocAnalysisWorkflow, RiskAdhocAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		From:         time.Now().Add(-24 * time.Hour),
		To:           time.Now(),
		Cursor:       cursor,
		Progress: RiskAdhocProgress{
			TotalMessages:      20_000,
			DispatchedMessages: 10_000,
			ProcessedMessages:  10_000,
			Findings:           7,
			BatchesCompleted:   100,
			BatchesFailed:      0,
			Policies:           1,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 0, countCalls, "the total is computed once on the first run only")

	var progress RiskAdhocProgress
	require.NoError(t, env.GetWorkflowResult(&progress))
	assert.Equal(t, int64(20_000), progress.TotalMessages, "carried progress must survive the resumed run")
	assert.Equal(t, int64(7), progress.Findings)
}

func TestRiskAdhocWorkflow_BatchFailureCountedNotFatal(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	messageIDs := make([]uuid.UUID, 150)
	for i := range messageIDs {
		messageIDs[i] = uuid.New()
	}
	analyzeCalls := 0

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.CountAdhocArgs) (int64, error) {
			return int64(len(messageIDs)), nil
		},
		activity.RegisterOptions{Name: "CountAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchAdhocArgs) (*risk_analysis.FetchAdhocResult, error) {
			return &risk_analysis.FetchAdhocResult{
				MessageIDs: messageIDs,
				Policies:   []risk_analysis.PolicyForAnalysis{{ID: uuid.New(), OrganizationID: "org1", Version: 1}},
			}, nil
		},
		activity.RegisterOptions{Name: "FetchAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_analysis.AnalyzeBatchArgs) (*risk_analysis.AnalyzeBatchResult, error) {
			analyzeCalls++
			// Fail the trailing 50-message batch; the full 100-message batch succeeds.
			if len(args.MessageIDs) == 50 {
				return nil, temporal.NewNonRetryableApplicationError("scan failed", "scanFailure", nil)
			}
			return &risk_analysis.AnalyzeBatchResult{Processed: len(args.MessageIDs), Findings: 0}, nil
		},
		activity.RegisterOptions{Name: "AnalyzeBatch"},
	)

	env.ExecuteWorkflow(RiskAdhocAnalysisWorkflow, RiskAdhocAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		From:         time.Now().Add(-24 * time.Hour),
		To:           time.Now(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "batch failures are counted, not fatal")

	var progress RiskAdhocProgress
	require.NoError(t, env.GetWorkflowResult(&progress))
	assert.Equal(t, int64(1), progress.BatchesFailed)
	assert.Equal(t, int64(1), progress.BatchesCompleted)
	assert.Equal(t, int64(100), progress.ProcessedMessages)
}

func TestRiskAdhocWorkflow_ProgressQuery(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.CountAdhocArgs) (int64, error) {
			return 42, nil
		},
		activity.RegisterOptions{Name: "CountAdhocAnalysisMessages"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ risk_analysis.FetchAdhocArgs) (*risk_analysis.FetchAdhocResult, error) {
			return &risk_analysis.FetchAdhocResult{MessageIDs: nil, Policies: nil}, nil
		},
		activity.RegisterOptions{Name: "FetchAdhocAnalysisMessages"},
	)

	env.ExecuteWorkflow(RiskAdhocAnalysisWorkflow, RiskAdhocAnalysisParams{
		ProjectID:    uuid.New(),
		RiskPolicyID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		From:         time.Now().Add(-time.Hour),
		To:           time.Now(),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(RiskAdhocProgressQueryType)
	require.NoError(t, err)
	var progress RiskAdhocProgress
	require.NoError(t, val.Get(&progress))
	assert.Equal(t, int64(42), progress.TotalMessages)
}
