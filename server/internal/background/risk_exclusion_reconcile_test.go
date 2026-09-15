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
// one immediate reconcile over the whole retention window, then a delayed
// re-run bounded to the most recent ClickHouse partition days, which catches
// findings ingested with stale exclusion flags during the writer cache's TTL
// window.
func TestRiskExclusionReconcileWorkflowRunsSecondSweep(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	projectID := uuid.New()
	exclusionID := uuid.New()
	var windows []int
	env.RegisterActivityWithOptions(
		func(_ context.Context, args risk_exclusion.ReconcileArgs) error {
			require.Equal(t, projectID, args.ProjectID)
			require.Equal(t, exclusionID, args.ExclusionID)
			windows = append(windows, args.WindowDays)
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
	require.Equal(t, []int{0, reconcileSecondSweepWindowDays}, windows,
		"the first sweep covers full retention, the second only the recent days")
}
