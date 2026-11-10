package background

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubIntegrationCredentialsBackfiller struct {
	result integrationcredentials.BackfillResult
	err    error
}

func (s stubIntegrationCredentialsBackfiller) Run(context.Context) (integrationcredentials.BackfillResult, error) {
	return s.result, s.err
}

func TestIntegrationCredentialsBackfill_WithoutBackfillerDoesNothing(t *testing.T) {
	t.Parallel()

	result, err := NewIntegrationCredentialsBackfillActivities(testenv.NewLogger(t), nil).Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, integrationcredentials.BackfillResult{Organizations: 0, Sealed: 0, Unreadable: 0, Failed: 0}, result)
}

func TestIntegrationCredentialsBackfill_ReportsTheRun(t *testing.T) {
	t.Parallel()

	want := integrationcredentials.BackfillResult{Organizations: 2, Sealed: 5, Unreadable: 1, Failed: 1}
	result, err := NewIntegrationCredentialsBackfillActivities(testenv.NewLogger(t), stubIntegrationCredentialsBackfiller{result: want, err: nil}).Backfill(t.Context())
	require.NoError(t, err)
	require.Equal(t, want, result)
}

func TestIntegrationCredentialsBackfillWorkflow_RetriesAFailedRunOnce(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	failure := errors.New("database unavailable")
	activities := NewIntegrationCredentialsBackfillActivities(testenv.NewLogger(t), stubIntegrationCredentialsBackfiller{result: integrationcredentials.BackfillResult{Organizations: 0, Sealed: 0, Unreadable: 0, Failed: 0}, err: failure})
	calls := 0
	env.RegisterActivityWithOptions(func(ctx context.Context) (integrationcredentials.BackfillResult, error) {
		calls++
		return activities.Backfill(ctx)
	}, activity.RegisterOptions{Name: IntegrationCredentialsBackfillActivityName})

	env.ExecuteWorkflow(IntegrationCredentialsBackfillWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 2, calls, "the activity is attempted twice before the run fails")
}

func TestIntegrationCredentialsBackfillWorkflow_RunsTheActivityOnce(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context) (integrationcredentials.BackfillResult, error) {
		calls++
		return integrationcredentials.BackfillResult{Organizations: 1, Sealed: 3, Unreadable: 0, Failed: 0}, nil
	}, activity.RegisterOptions{Name: IntegrationCredentialsBackfillActivityName})

	env.ExecuteWorkflow(IntegrationCredentialsBackfillWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)

	var result integrationcredentials.BackfillResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 3, result.Sealed)
}
