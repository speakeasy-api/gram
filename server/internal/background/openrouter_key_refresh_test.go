package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestPaygOpenRouterChatKeyReconcileWorkflowUsesCurrentStateActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var received activities.ReconcilePaygOpenRouterChatKeyArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.ReconcilePaygOpenRouterChatKeyArgs) error {
			received = args
			return nil
		},
		activity.RegisterOptions{Name: "ReconcilePaygOpenRouterChatKey"},
	)

	env.ExecuteWorkflow(PaygOpenRouterChatKeyReconcileWorkflow, ReconcilePaygOpenRouterChatKeyParams{OrganizationID: "organization_placeholder"})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, "organization_placeholder", received.OrganizationID)
}

func TestPaygOpenRouterChatKeyReconcileWorkflowRetriesExternalFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReconcilePaygOpenRouterChatKeyArgs) error {
			attempts.Add(1)
			return errors.New("upstream unavailable")
		},
		activity.RegisterOptions{Name: "ReconcilePaygOpenRouterChatKey"},
	)

	env.ExecuteWorkflow(PaygOpenRouterChatKeyReconcileWorkflow, ReconcilePaygOpenRouterChatKeyParams{OrganizationID: "organization_placeholder"})

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "reconcile PAYG OpenRouter chat key")
	require.EqualValues(t, 5, attempts.Load())
}
