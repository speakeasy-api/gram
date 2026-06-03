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
	env.SetWorkflowRunTimeout(refreshBillingUsageActivityWorstCaseRetryWindow + refreshBillingUsagesWaitInterval)

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

	env.ExecuteWorkflow(RefreshBillingUsageWorkflow, RefreshBillingUsageInput{
		OrgIDs:           orgIDs,
		StartIndex:       0,
		FailedBatchCount: 0,
		FailedOrgCount:   0,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, refreshCallCount)
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
}
