package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestOpenRouterCreditsMetrics_MonitorsEnterpriseAndPAYG(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var collected activities.CollectOpenRouterCreditsMetricsArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.CollectOpenRouterCreditsMetricsArgs) ([]activities.OpenRouterCreditsMetric, error) {
			collected = args
			return nil, nil
		},
		activity.RegisterOptions{Name: "CollectOpenRouterCreditsMetrics"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, []activities.OpenRouterCreditsMetric) error { return nil },
		activity.RegisterOptions{Name: "FireOpenRouterCreditsMetrics"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, []activities.OpenRouterCreditsMetric) error { return nil },
		activity.RegisterOptions{Name: "MaybeSendOpenRouterCreditsAlerts"},
	)

	env.ExecuteWorkflow(CollectOpenRouterCreditsMetricsWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.ElementsMatch(t, []string{"enterprise", "payg"}, collected.AccountTypes)
}
