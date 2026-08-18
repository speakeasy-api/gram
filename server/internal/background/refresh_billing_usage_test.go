package background

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestRefreshBillingUsageWorkflow_ActivityStartToCloseTimeouts(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	orgIDs := []string{"org_1"}

	// The test environment stamps Deadline from the real clock, so the
	// difference carries sub-second scheduling skew; round it away.
	startToClose := func(ctx context.Context) time.Duration {
		info := activity.GetInfo(ctx)
		return info.Deadline.Sub(info.StartedTime).Round(time.Minute)
	}

	var refreshTimeout time.Duration
	env.RegisterActivityWithOptions(
		func(ctx context.Context, _ []string) error {
			refreshTimeout = startToClose(ctx)
			return nil
		},
		activity.RegisterOptions{Name: "RefreshBillingUsage"},
	)

	var snapshotTimeout time.Duration
	env.RegisterActivityWithOptions(
		func(ctx context.Context, _ []string) error {
			snapshotTimeout = startToClose(ctx)
			return nil
		},
		activity.RegisterOptions{Name: "SnapshotBillingCycleUsage"},
	)

	var reportTimeout time.Duration
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.ReportTUMUsageToStripeInput) error {
			reportTimeout = startToClose(ctx)
			require.Equal(t, orgIDs, input.OrganizationIDs)
			require.False(t, input.Now.IsZero())
			return nil
		},
		activity.RegisterOptions{Name: "ReportTUMUsageToStripe"},
	)

	var forwardTimeout time.Duration
	env.RegisterActivityWithOptions(
		func(ctx context.Context, _ []string) error {
			forwardTimeout = startToClose(ctx)
			return nil
		},
		activity.RegisterOptions{Name: "ForwardTokenUsageToPostHog"},
	)

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           orgIDs,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2*time.Minute, refreshTimeout,
		"Polar refresh needs headroom for slow serialized /quantities meter queries")
	require.Equal(t, 5*time.Minute, snapshotTimeout,
		"snapshot keeps its wider first-run backfill deadline")
	require.Equal(t, 3*time.Minute, reportTimeout,
		"Stripe reporting covers a fully serial degraded batch")
	require.Equal(t, time.Minute, forwardTimeout,
		"posthog forward keeps a short deadline so the batch worst-case window stays inside the run timeout")
}

func TestRefreshBillingUsageWorkflow_ContinuesAsNewNearRunTimeout(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkflowRunTimeout(refreshBillingUsageBatchWorstCaseRetryWindow + refreshBillingUsagesWaitInterval)

	orgIDs := make([]string, (billingUsagePauseEveryBatches+1)*refreshBillingUsageBatchSize)
	for i := range orgIDs {
		orgIDs[i] = "org_" + strconv.Itoa(i)
	}

	getAllCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]string, error) {
			getAllCallCount++
			return orgIDs, nil
		},
		activity.RegisterOptions{Name: "GetAllOrganizations"},
	)

	refreshCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			refreshCallCount++
			require.NotEmpty(t, batch)
			require.LessOrEqual(t, len(batch), refreshBillingUsageBatchSize)
			return nil
		},
		activity.RegisterOptions{Name: "RefreshBillingUsage"},
	)

	snapshotCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			snapshotCallCount++
			require.NotEmpty(t, batch)
			require.LessOrEqual(t, len(batch), refreshBillingUsageBatchSize)
			return nil
		},
		activity.RegisterOptions{Name: "SnapshotBillingCycleUsage"},
	)
	reportCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ReportTUMUsageToStripeInput) error {
			reportCallCount++
			require.NotEmpty(t, input.OrganizationIDs)
			return nil
		},
		activity.RegisterOptions{Name: "ReportTUMUsageToStripe"},
	)

	forwardCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			forwardCallCount++
			require.NotEmpty(t, batch)
			require.LessOrEqual(t, len(batch), refreshBillingUsageBatchSize)
			return nil
		},
		activity.RegisterOptions{Name: "ForwardTokenUsageToPostHog"},
	)

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           nil,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, "RefreshBillingUsageWorkflow", continueAsNewErr.WorkflowType.Name)
	require.Equal(t, 1, getAllCallCount)
	require.Equal(t, billingUsagePauseEveryBatches, refreshCallCount)
	require.Equal(t, refreshCallCount, snapshotCallCount, "every batch gets a snapshot activity")
	require.Equal(t, refreshCallCount, reportCallCount, "every batch reports durable TUM usage")
	require.Equal(t, refreshCallCount, forwardCallCount, "every batch forwards token usage to posthog")

	var nextInput RefreshBillingUsageInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))
	require.Equal(t, orgIDs, nextInput.OrgIDs)
	require.Equal(t, billingUsagePauseEveryBatches*refreshBillingUsageBatchSize, nextInput.StartIndex)
	require.Zero(t, nextInput.FailedBatchCount)
	require.Zero(t, nextInput.FailedOrgCount)
}

func TestRefreshBillingUsageWorkflow_ContinuesAsNewBeforeFirstBatchWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	// A run timeout smaller than the batch worst-case window means there is
	// never room for a first batch: the workflow must continue as new right
	// after loading organizations instead of starting a batch it cannot
	// finish within the run timeout.
	env.SetWorkflowRunTimeout(refreshBillingUsageBatchWorstCaseRetryWindow - time.Minute)

	orgIDs := make([]string, 2*refreshBillingUsageBatchSize)
	for i := range orgIDs {
		orgIDs[i] = "org_" + strconv.Itoa(i)
	}

	getAllCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]string, error) {
			getAllCallCount++
			return orgIDs, nil
		},
		activity.RegisterOptions{Name: "GetAllOrganizations"},
	)

	refreshCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []string) error {
			refreshCallCount++
			return nil
		},
		activity.RegisterOptions{Name: "RefreshBillingUsage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []string) error {
			return nil
		},
		activity.RegisterOptions{Name: "SnapshotBillingCycleUsage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReportTUMUsageToStripeInput) error { return nil },
		activity.RegisterOptions{Name: "ReportTUMUsageToStripe"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []string) error {
			return nil
		},
		activity.RegisterOptions{Name: "ForwardTokenUsageToPostHog"},
	)

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           nil,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, 1, getAllCallCount)
	require.Zero(t, refreshCallCount, "no batch may start without a full worst-case retry window remaining")

	var nextInput RefreshBillingUsageInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))
	require.Equal(t, orgIDs, nextInput.OrgIDs)
	require.Zero(t, nextInput.StartIndex, "the continued run must resume from the batch that never started")
}

func TestRefreshBillingUsageWorkflow_ContinuedRunAlwaysMakesProgress(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	// A run timeout at or below the batch worst-case window makes the budget
	// check report "no room" from the very start of every run. A continued
	// run (orgs already loaded) must still process at least one batch under
	// that configuration, or back-to-back continue-as-new calls with an
	// unchanged start index would livelock the workflow with zero progress.
	env.SetWorkflowRunTimeout(refreshBillingUsageBatchWorstCaseRetryWindow - time.Minute)

	orgIDs := make([]string, 2*refreshBillingUsageBatchSize)
	for i := range orgIDs {
		orgIDs[i] = "org_" + strconv.Itoa(i)
	}

	refreshCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []string) error {
			refreshCallCount++
			return nil
		},
		activity.RegisterOptions{Name: "RefreshBillingUsage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []string) error {
			return nil
		},
		activity.RegisterOptions{Name: "SnapshotBillingCycleUsage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReportTUMUsageToStripeInput) error { return nil },
		activity.RegisterOptions{Name: "ReportTUMUsageToStripe"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ []string) error {
			return nil
		},
		activity.RegisterOptions{Name: "ForwardTokenUsageToPostHog"},
	)

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           orgIDs,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.GreaterOrEqual(t, refreshCallCount, 1, "a continued run must process at least one batch before continuing as new")

	var nextInput RefreshBillingUsageInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))
	require.Positive(t, nextInput.StartIndex, "the continued run must advance the start index")
}

func TestRefreshBillingUsageWorkflow_FailingBatchDoesNotAbortRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	orgIDs := []string{
		"org_1",
		"org_2",
		"org_3",
		"org_4",
		"org_5",
		"org_6",
	}

	refreshCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			refreshCallCount++
			switch refreshCallCount {
			case 1:
				require.Equal(t, orgIDs[:refreshBillingUsageBatchSize], batch)
				return temporal.NewNonRetryableApplicationError("polar failed", "", nil)
			case 2:
				require.Equal(t, orgIDs[refreshBillingUsageBatchSize:], batch)
				return nil
			default:
				t.Fatalf("unexpected refresh call %d", refreshCallCount)
				return nil
			}
		},
		activity.RegisterOptions{Name: "RefreshBillingUsage"},
	)

	snapshotCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			snapshotCallCount++
			require.NotEmpty(t, batch)
			return nil
		},
		activity.RegisterOptions{Name: "SnapshotBillingCycleUsage"},
	)
	reportCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ReportTUMUsageToStripeInput) error {
			reportCallCount++
			require.NotEmpty(t, input.OrganizationIDs)
			return nil
		},
		activity.RegisterOptions{Name: "ReportTUMUsageToStripe"},
	)
	forwardCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			forwardCallCount++
			require.NotEmpty(t, batch)
			return nil
		},
		activity.RegisterOptions{Name: "ForwardTokenUsageToPostHog"},
	)

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           orgIDs,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, refreshCallCount)
	require.Equal(t, 2, snapshotCallCount, "snapshots still run when the Polar refresh batch fails")
	require.Equal(t, 2, reportCallCount, "Stripe reporting still runs when the Polar refresh batch fails")
	require.Equal(t, 2, forwardCallCount, "posthog forwarding still runs when the Polar refresh batch fails")
}

func TestFailedTUMReportingOrganizationCount(t *testing.T) {
	t.Parallel()

	err := temporal.NewApplicationErrorWithOptions(
		"report failed",
		activities.ErrTypeTUMStripeReporting,
		temporal.ApplicationErrorOptions{Details: []any{activities.ReportTUMUsageToStripeFailureDetails{
			FailedOrganizationCount: 1,
		}}},
	)
	require.Equal(t, 1, failedTUMReportingOrganizationCount(err, refreshBillingUsageBatchSize))
	require.Equal(t, refreshBillingUsageBatchSize, failedTUMReportingOrganizationCount(errors.New("unknown"), refreshBillingUsageBatchSize))
}

func TestRefreshBillingUsageWorkflow_SleepCancellationFailsRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	orgIDs := []string{
		"org_1",
		"org_2",
		"org_3",
		"org_4",
		"org_5",
		"org_6",
		"org_7",
		"org_8",
		"org_9",
		"org_10",
		"org_11",
	}

	refreshCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			refreshCallCount++
			require.NotEmpty(t, batch)
			return nil
		},
		activity.RegisterOptions{Name: "RefreshBillingUsage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			return nil
		},
		activity.RegisterOptions{Name: "SnapshotBillingCycleUsage"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReportTUMUsageToStripeInput) error { return nil },
		activity.RegisterOptions{Name: "ReportTUMUsageToStripe"},
	)
	forwardCallCount := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, batch []string) error {
			forwardCallCount++
			require.NotEmpty(t, batch)
			return nil
		},
		activity.RegisterOptions{Name: "ForwardTokenUsageToPostHog"},
	)
	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, refreshBillingUsagesWaitInterval/2)

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           orgIDs,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, billingUsagePauseEveryBatches, refreshCallCount)
	require.Equal(t, refreshCallCount, forwardCallCount, "every completed batch forwarded token usage before the cancellation")
}
