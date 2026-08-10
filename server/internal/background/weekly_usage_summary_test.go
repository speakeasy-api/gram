package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func weeklyUsageSummaryTestTargets() []activities.WeeklyUsageSummaryTarget {
	return []activities.WeeklyUsageSummaryTarget{
		{OrganizationID: "org-a", OrganizationName: "Org A", OrganizationSlug: "org-a", AlertEmail: "a@example.com", AnchorDay: 1},
		{OrganizationID: "org-b", OrganizationName: "Org B", OrganizationSlug: "org-b", AlertEmail: "b@example.com", AnchorDay: 1},
		{OrganizationID: "org-c", OrganizationName: "Org C", OrganizationSlug: "org-c", AlertEmail: "c@example.com", AnchorDay: 15},
	}
}

func TestWeeklyUsageSummaryWorkflow_SweepsAllTargets(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]activities.WeeklyUsageSummaryTarget, error) {
			return weeklyUsageSummaryTestTargets(), nil
		},
		activity.RegisterOptions{Name: "ListWeeklyUsageSummaryTargets"},
	)

	var sent []activities.SendWeeklyUsageSummaryArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.SendWeeklyUsageSummaryArgs) error {
			sent = append(sent, args)
			return nil
		},
		activity.RegisterOptions{Name: "SendWeeklyUsageSummary"},
	)

	env.ExecuteWorkflow(WeeklyUsageSummaryWorkflow, WeeklyUsageSummaryInput{
		Targets:     nil,
		StartIndex:  0,
		FailedCount: 0,
		RunTime:     time.Time{},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, sent, 3)
	for i, target := range weeklyUsageSummaryTestTargets() {
		require.Equal(t, target, sent[i].Target)
		// One sweep must anchor every send to the same run time so cycle
		// math and idempotency keys agree across targets.
		require.Equal(t, sent[0].RunTime, sent[i].RunTime)
		require.False(t, sent[i].RunTime.IsZero())
	}
}

func TestWeeklyUsageSummaryWorkflow_ContinuesAsNewNearRunTimeout(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	// With the run timeout equal to the reserved activity budget, the guard
	// trips right after the first send.
	env.SetWorkflowRunTimeout(weeklyUsageSummaryActivityBudget)

	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]activities.WeeklyUsageSummaryTarget, error) {
			return weeklyUsageSummaryTestTargets(), nil
		},
		activity.RegisterOptions{Name: "ListWeeklyUsageSummaryTargets"},
	)

	sends := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.SendWeeklyUsageSummaryArgs) error {
			sends++
			return nil
		},
		activity.RegisterOptions{Name: "SendWeeklyUsageSummary"},
	)

	env.ExecuteWorkflow(WeeklyUsageSummaryWorkflow, WeeklyUsageSummaryInput{
		Targets:     nil,
		StartIndex:  0,
		FailedCount: 0,
		RunTime:     time.Time{},
	})

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, "WeeklyUsageSummaryWorkflow", continueAsNewErr.WorkflowType.Name)
	require.Equal(t, 1, sends)

	var nextInput WeeklyUsageSummaryInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueAsNewErr.Input, &nextInput))
	require.Equal(t, weeklyUsageSummaryTestTargets(), nextInput.Targets)
	require.Equal(t, 1, nextInput.StartIndex)
	require.Equal(t, 0, nextInput.FailedCount)
	require.False(t, nextInput.RunTime.IsZero(), "run time must carry across ContinueAsNew so idempotency keys stay stable")
}

func TestWeeklyUsageSummaryWorkflow_ResumesFromStartIndexWithoutRelisting(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context) ([]activities.WeeklyUsageSummaryTarget, error) {
			listCalls++
			return nil, nil
		},
		activity.RegisterOptions{Name: "ListWeeklyUsageSummaryTargets"},
	)

	var sent []activities.SendWeeklyUsageSummaryArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.SendWeeklyUsageSummaryArgs) error {
			sent = append(sent, args)
			return nil
		},
		activity.RegisterOptions{Name: "SendWeeklyUsageSummary"},
	)

	runTime := time.Date(2026, 8, 3, 16, 0, 0, 0, time.UTC)
	env.ExecuteWorkflow(WeeklyUsageSummaryWorkflow, WeeklyUsageSummaryInput{
		Targets:     weeklyUsageSummaryTestTargets(),
		StartIndex:  2,
		FailedCount: 1,
		RunTime:     runTime,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Zero(t, listCalls, "a continued run must reuse the carried target list")
	require.Len(t, sent, 1)
	require.Equal(t, "org-c", sent[0].Target.OrganizationID)
	require.Equal(t, runTime, sent[0].RunTime)
}
