package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion"
)

// TestRiskExclusionReconcileWorkflowRunsSecondSweep pins the two-pass shape:
// one immediate reconcile, then a delayed re-run that catches findings
// ingested with stale exclusion flags during the writer cache's TTL window.
func TestRiskExclusionReconcileWorkflowRunsSecondSweep(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	exclusionID := uuid.New()
	runs := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_exclusion.ReconcileArgs) error {
			require.Equal(t, projectID, args.ProjectID)
			require.Equal(t, exclusionID, args.ExclusionID)
			runs++
			return nil
		},
		activity.RegisterOptions{Name: "ReconcileExclusion"},
	)

	env.ExecuteWorkflow(RiskExclusionReconcileWorkflow, RiskExclusionReconcileParams{
		ProjectID:   projectID,
		ExclusionID: exclusionID,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, runs)
}
