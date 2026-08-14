package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestCollectOpenRouterDailySpendWorkflowCollectsLastThreeCompletedUTCDays(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, time.August, 14, 23, 30, 0, 0, time.FixedZone("UTC-7", -7*60*60)))

	var received activities.CollectOpenRouterDailySpendArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.CollectOpenRouterDailySpendArgs) error {
			received = args
			return nil
		},
		activity.RegisterOptions{Name: "CollectOpenRouterDailySpend"},
	)

	env.ExecuteWorkflow(CollectOpenRouterDailySpendWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC), received.StartDay)
	require.Equal(t, time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC), received.EndDay)
}

func TestCollectOpenRouterDailySpendWorkflowPropagatesFailureAfterRetries(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, activities.CollectOpenRouterDailySpendArgs) error {
			attempts.Add(1)
			return errors.New("OpenRouter unavailable")
		},
		activity.RegisterOptions{Name: "CollectOpenRouterDailySpend"},
	)

	env.ExecuteWorkflow(CollectOpenRouterDailySpendWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "collect openrouter daily spend")
	require.EqualValues(t, openRouterDailySpendActivityMaxAttempts, attempts.Load())
}

func TestOpenRouterDailySpendScheduleOptions(t *testing.T) {
	t.Parallel()

	options := openRouterDailySpendScheduleOptions("test-task-queue")

	require.Equal(t, openRouterDailySpendScheduleID, options.ID)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, options.Overlap)
	require.Len(t, options.Spec.Calendars, 1)
	require.Equal(t, []client.ScheduleRange{{Start: 4, End: 0, Step: 0}}, options.Spec.Calendars[0].Hour)

	action, ok := options.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	require.Equal(t, openRouterDailySpendScheduledWorkflowID, action.ID)
	require.Equal(t, "test-task-queue", action.TaskQueue)
	require.Equal(t, openRouterDailySpendWorkflowRunTimeout, action.WorkflowRunTimeout)
}
