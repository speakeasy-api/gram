package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestDemoteExpiredTrialsWorkflow_DemotesEveryExpiredTrial(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]string, error) {
			return []string{"org_1", "org_2", "org_3"}, nil
		},
		activity.RegisterOptions{Name: "ListExpiredTrials"},
	)

	var demoted []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.DemoteExpiredTrialArgs) error {
			demoted = append(demoted, args.OrganizationID)
			return nil
		},
		activity.RegisterOptions{Name: "DemoteExpiredTrial"},
	)

	env.ExecuteWorkflow(DemoteExpiredTrialsWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{"org_1", "org_2", "org_3"}, demoted)
}

// One organization whose provider calls keep failing must not stop the others
// from being locked out, and the run still has to report the failure.
func TestDemoteExpiredTrialsWorkflow_FailedDemotionDoesNotAbortSweep(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]string, error) {
			return []string{"org_1", "org_2", "org_3"}, nil
		},
		activity.RegisterOptions{Name: "ListExpiredTrials"},
	)

	var demoted []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.DemoteExpiredTrialArgs) error {
			if args.OrganizationID == "org_2" {
				return temporal.NewNonRetryableApplicationError("openrouter unavailable", "", nil)
			}
			demoted = append(demoted, args.OrganizationID)
			return nil
		},
		activity.RegisterOptions{Name: "DemoteExpiredTrial"},
	)

	env.ExecuteWorkflow(DemoteExpiredTrialsWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "1 of 3 organizations failed")
	require.Equal(t, []string{"org_1", "org_3"}, demoted)
}
