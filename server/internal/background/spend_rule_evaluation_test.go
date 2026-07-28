package background

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	spend_rules "github.com/speakeasy-api/gram/server/internal/background/activities/spend_rules"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
)

func TestSpendRuleEvaluationWorkflowContinuesWithRemainingOrgs(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	orgs := []string{"org_01", "org_02", "org_03", "org_04", "org_05", "org_06"}
	env.RegisterActivityWithOptions(
		func(context.Context) ([]string, error) {
			return orgs, nil
		},
		activity.RegisterOptions{Name: "ListSpendRuleOrgs"},
	)

	evaluated := make([]string, 0, spendRuleEvaluationOrgPageSize)
	env.RegisterActivityWithOptions(
		func(_ context.Context, args spend_rules.EvaluateOrgArgs) error {
			evaluated = append(evaluated, args.OrganizationID)
			return nil
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleEvaluationWorkflow, SpendRuleEvaluationParams{
		OrganizationIDs: nil,
		EvaluatedCount:  0,
		ErrorCount:      0,
	})

	require.True(t, env.IsWorkflowCompleted())
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "SpendRuleEvaluationWorkflow", canErr.WorkflowType.Name)
	require.Equal(t, orgs[:spendRuleEvaluationOrgPageSize], evaluated)
}

func TestSpendRuleEvaluationWorkflowContinuesBeforeReportingPageErrors(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	orgs := []string{"org_01", "org_02", "org_03", "org_04", "org_05", "org_06"}
	env.RegisterActivityWithOptions(
		func(context.Context) ([]string, error) {
			return orgs, nil
		},
		activity.RegisterOptions{Name: "ListSpendRuleOrgs"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, spend_rules.EvaluateOrgArgs) error {
			return errors.New("evaluate failed")
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleEvaluationWorkflow, SpendRuleEvaluationParams{
		OrganizationIDs: nil,
		EvaluatedCount:  0,
		ErrorCount:      0,
	})

	require.True(t, env.IsWorkflowCompleted())
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "SpendRuleEvaluationWorkflow", canErr.WorkflowType.Name)
}

func TestSpendRuleEvaluationWorkflowReportsAggregatedErrorsOnFinalPage(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(context.Context, spend_rules.EvaluateOrgArgs) error {
			return errors.New("evaluate failed")
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleEvaluationWorkflow, SpendRuleEvaluationParams{
		OrganizationIDs: []string{"org_06"},
		EvaluatedCount:  5,
		ErrorCount:      2,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "spend rule evaluation failed for 3 of 6 orgs")
}

func TestSpendRuleOrgEvaluationWorkflowID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:spend-rule-eval:org_01HZ", buildSpendRuleOrgEvaluationWorkflowID("org_01HZ"))
	require.Equal(t, "v1:spend-rule-eval/signal", spendRuleOrgEvaluationDebounceSignal("org_01HZ"))
}

func TestSpendRuleActorEvaluationWorkflowIDUsesUserIDOnly(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:spend-rule-actor-eval:org_01HZ:user_123", buildSpendRuleActorEvaluationWorkflowID(spendrules.ActorEvaluationSignal{
		OrganizationID: "org_01HZ",
		UserID:         "user_123",
		Email:          "ada@acme.com",
	}))
	require.Equal(t, "v1:spend-rule-actor-eval:org_01HZ:user_123", buildSpendRuleActorEvaluationWorkflowID(spendrules.ActorEvaluationSignal{
		OrganizationID: "org_01HZ",
		UserID:         "user_123",
		Email:          "other@acme.com",
	}))
}

func TestSpendRuleOrgEvaluationWorkflowDebounced_CompletesWithoutSignals(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	evalCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, args spend_rules.EvaluateOrgArgs) error {
			evalCalls++
			require.Equal(t, "org_01HZ", args.OrganizationID)
			return nil
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleOrgEvaluationWorkflowDebounced, "org_01HZ")

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, evalCalls)
}

func TestSpendRuleOrgEvaluationWorkflowDebounced_StartSignalDoesNotSelfLoop(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Simulate SignalWithStart: the signal that started the run is queued
	// before workflow code executes. The wrapper must drain it up front so
	// the post-run check does not immediately ContinueAsNew.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(spendRuleOrgEvaluationDebounceSignal("org_01HZ"), "enqueue")
	}, 0)

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ spend_rules.EvaluateOrgArgs) error {
			return nil
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleOrgEvaluationWorkflowDebounced, "org_01HZ")

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "start signal must be drained at top; should complete, not ContinueAsNew")
}

func TestSpendRuleOrgEvaluationWorkflowDebounced_SignalMidRunContinuesAsNew(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// A signal landing while evaluation is in flight (fresh usage committed
	// mid-run) must enqueue exactly one follow-up run.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ spend_rules.EvaluateOrgArgs) error {
			env.SignalWorkflow(spendRuleOrgEvaluationDebounceSignal("org_01HZ"), "enqueue")
			return nil
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleOrgEvaluationWorkflowDebounced, "org_01HZ")

	require.True(t, env.IsWorkflowCompleted())

	// The ContinueAsNew must target the debounced wrapper itself, not the
	// inner workflow, or the next run loses debounce semantics.
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "SpendRuleOrgEvaluationWorkflowDebounced", canErr.WorkflowType.Name)
}

func TestSpendRuleOrgEvaluationWorkflowDebounced_FailingRunWithSignalContinuesAsNew(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ spend_rules.EvaluateOrgArgs) error {
			env.SignalWorkflow(spendRuleOrgEvaluationDebounceSignal("org_01HZ"), "enqueue")
			return errors.New("evaluate failed")
		},
		activity.RegisterOptions{Name: "EvaluateOrgSpendRules"},
	)

	env.ExecuteWorkflow(SpendRuleOrgEvaluationWorkflowDebounced, "org_01HZ")

	require.True(t, env.IsWorkflowCompleted())
	var canErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &canErr)
	require.Equal(t, "SpendRuleOrgEvaluationWorkflowDebounced", canErr.WorkflowType.Name)
}
