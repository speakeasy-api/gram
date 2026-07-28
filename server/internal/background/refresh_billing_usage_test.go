package background

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

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
	require.Equal(t, refreshCallCount, forwardCallCount, "every batch forwards token usage to posthog")

	var nextInput RefreshBillingUsageInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))
	require.Equal(t, orgIDs, nextInput.OrgIDs)
	require.Equal(t, billingUsagePauseEveryBatches*refreshBillingUsageBatchSize, nextInput.StartIndex)
	require.Zero(t, nextInput.FailedBatchCount)
	require.Zero(t, nextInput.FailedOrgCount)
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
	require.Equal(t, 2, forwardCallCount, "posthog forwarding still runs when the Polar refresh batch fails")
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
