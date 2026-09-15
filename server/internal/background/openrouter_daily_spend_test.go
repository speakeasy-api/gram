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

func TestCollectOpenRouterDailySpendWorkflowCollectsLastFourCompletedUTCDays(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, time.August, 14, 23, 30, 0, 0, time.FixedZone("UTC-7", -7*60*60)))

	var received activities.CollectOpenRouterDailySpendArgs
	var settled activities.SettleStripeInvoiceAllocationsArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.CollectOpenRouterDailySpendArgs) (activities.CollectOpenRouterDailySpendResult, error) {
			received = args
			return activities.CollectOpenRouterDailySpendResult{
				ReadyOrganizationIDs:         []string{"org-ready"},
				BillableKeyPolicyFingerprint: "4b4e792daf43040a6f92b112a281187144b92cc902d5b355056f28a0c2ad6894",
			}, nil
		},
		activity.RegisterOptions{Name: "CollectOpenRouterDailySpend"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.SettleStripeInvoiceAllocationsArgs) error {
			settled = args
			return nil
		},
		activity.RegisterOptions{Name: "SettleStripeInvoiceAllocations"},
	)

	env.ExecuteWorkflow(CollectOpenRouterDailySpendWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC), received.StartDay)
	require.Equal(t, time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC), received.EndDay)
	require.Equal(t, time.Date(2026, time.August, 15, 6, 30, 0, 0, time.UTC), settled.Now)
	require.True(t, settled.RestrictOpenRouterToReadyOrganizations)
	require.Equal(t, []string{"org-ready"}, settled.OpenRouterReadyOrganizationIDs)
	require.Equal(t, "4b4e792daf43040a6f92b112a281187144b92cc902d5b355056f28a0c2ad6894", settled.OpenRouterBillableKeyPolicyFingerprint)
}

func TestCollectOpenRouterDailySpendWorkflowPropagatesFailureAfterRetries(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	var settlementAttempts atomic.Int32
	var settled activities.SettleStripeInvoiceAllocationsArgs
	env.RegisterActivityWithOptions(
		func(context.Context, activities.CollectOpenRouterDailySpendArgs) (activities.CollectOpenRouterDailySpendResult, error) {
			attempts.Add(1)
			return activities.CollectOpenRouterDailySpendResult{}, errors.New("OpenRouter unavailable")
		},
		activity.RegisterOptions{Name: "CollectOpenRouterDailySpend"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.SettleStripeInvoiceAllocationsArgs) error {
			settlementAttempts.Add(1)
			settled = args
			return nil
		},
		activity.RegisterOptions{Name: "SettleStripeInvoiceAllocations"},
	)

	env.ExecuteWorkflow(CollectOpenRouterDailySpendWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "collect openrouter daily spend")
	require.EqualValues(t, openRouterDailySpendActivityMaxAttempts, attempts.Load())
	require.EqualValues(t, 1, settlementAttempts.Load(), "failed collection must still route independent TUM carries")
	require.True(t, settled.RestrictOpenRouterToReadyOrganizations)
	require.Empty(t, settled.OpenRouterReadyOrganizationIDs)
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
	require.Greater(t, openRouterDailySpendWorkflowRunTimeout, 2*openRouterDailySpendActivityScheduleToCloseTimeout)
}
