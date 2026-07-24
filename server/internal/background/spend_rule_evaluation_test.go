package background

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	spend_rules "github.com/speakeasy-api/gram/server/internal/background/activities/spend_rules"
)

func TestSpendRuleOrgEvaluationWorkflowID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:spend-rule-eval:org_01HZ", buildSpendRuleOrgEvaluationWorkflowID("org_01HZ"))
	require.Equal(t, "v1:spend-rule-eval/signal", spendRuleOrgEvaluationDebounceSignal("org_01HZ"))
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
