package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	env.ExecuteWorkflow(PaygOpenRouterChatKeyReconcileWorkflow, ReconcilePaygOpenRouterChatKeyParams{
		OrganizationID: "organization_placeholder",
		DesiredState:   openrouter.KeyDesiredStateDisabled,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, "organization_placeholder", received.OrganizationID)
	require.Equal(t, openrouter.KeyDesiredStateDisabled, received.DesiredState)
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

	env.ExecuteWorkflow(PaygOpenRouterChatKeyReconcileWorkflow, ReconcilePaygOpenRouterChatKeyParams{
		OrganizationID: "organization_placeholder",
		DesiredState:   openrouter.KeyDesiredStateDisabled,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "reconcile PAYG OpenRouter chat key")
	require.EqualValues(t, 5, attempts.Load())
}

func TestSchedulePaygOpenRouterChatKeyReconciliationDedupesExactEventReplay(t *testing.T) {
	t.Parallel()

	temporalClient := &temporalmocks.Client{}
	workflowOptions := mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
		return options.ID == "v1:openrouter-chat-key-reconcile:billing:event_placeholder" &&
			options.TaskQueue == "test" &&
			options.WorkflowIDReusePolicy == enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
	})
	workflowParams := ReconcilePaygOpenRouterChatKeyParams{
		OrganizationID: "organization_placeholder",
		DesiredState:   openrouter.KeyDesiredStateDisabled,
	}
	temporalClient.On("ExecuteWorkflow", mock.Anything, workflowOptions, mock.Anything, workflowParams).Return(nil, nil).Once()
	temporalClient.On("ExecuteWorkflow", mock.Anything, workflowOptions, mock.Anything, workflowParams).Return(
		nil,
		serviceerror.NewWorkflowExecutionAlreadyStarted("duplicate", "request_placeholder", "run_placeholder"),
	).Once()

	scheduler := &OpenRouterKeyRefresher{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	require.NoError(t, scheduler.SchedulePaygOpenRouterChatKeyReconciliation(t.Context(), "event_placeholder", "organization_placeholder", openrouter.KeyDesiredStateDisabled))
	require.NoError(t, scheduler.SchedulePaygOpenRouterChatKeyReconciliation(t.Context(), "event_placeholder", "organization_placeholder", openrouter.KeyDesiredStateDisabled))
	temporalClient.AssertExpectations(t)
}

func TestSetOpenRouterSpendCapNormalizesLegacyEmptyKeyType(t *testing.T) {
	t.Parallel()

	temporalClient := &temporalmocks.Client{}
	run := &temporalmocks.WorkflowRun{}
	run.On("Get", mock.Anything, mock.Anything).Return(nil).Once()

	workflowOptions := mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
		return options.ID == "v1:openrouter-spend-cap:chat:operation_placeholder" &&
			options.WorkflowIDReusePolicy == enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
	})
	workflowParams := OpenRouterSpendCapParams{
		OperationID:      "operation_placeholder",
		OrganizationID:   "organization_placeholder",
		KeyType:          string(openrouter.KeyTypeChat),
		Limit:            100,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "user_placeholder"),
		ActorDisplayName: nil,
	}
	temporalClient.On("ExecuteWorkflow", mock.Anything, workflowOptions, mock.Anything, workflowParams).Return(run, nil).Once()

	scheduler := &OpenRouterKeyRefresher{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	require.NoError(t, scheduler.SetOpenRouterSpendCap(
		t.Context(),
		"operation_placeholder",
		"organization_placeholder",
		openrouter.KeyType(""),
		100,
		workflowParams.Actor,
		nil,
	))
	temporalClient.AssertExpectations(t)
	run.AssertExpectations(t)
}

func TestOpenRouterSpendCapWorkflowsEnforcePolicyMode(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	customerEnv := suite.NewTestWorkflowEnvironment()
	customerEnv.RegisterActivityWithOptions(func(_ context.Context, args activities.SetOpenRouterSpendCapArgs) (int, error) {
		require.False(t, args.BypassPolicy)
		return 100, nil
	}, activity.RegisterOptions{Name: "SetOpenRouterSpendCap"})
	params := OpenRouterSpendCapParams{
		OperationID: "operation_placeholder", OrganizationID: "organization_placeholder",
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "user_placeholder"), BypassPolicy: true,
	}
	customerEnv.ExecuteWorkflow(OpenRouterSpendCapWorkflow, params)
	require.NoError(t, customerEnv.GetWorkflowError())

	adminEnv := suite.NewTestWorkflowEnvironment()
	adminEnv.RegisterActivityWithOptions(func(_ context.Context, args activities.SetOpenRouterSpendCapArgs) (int, error) {
		require.True(t, args.BypassPolicy)
		return 100, nil
	}, activity.RegisterOptions{Name: "SetOpenRouterSpendCap"})
	params.BypassPolicy = false
	adminEnv.ExecuteWorkflow(AdminOpenRouterSpendCapWorkflow, params)
	require.NoError(t, adminEnv.GetWorkflowError())
}

func TestSetAdminOpenRouterSpendCapBypassesPolicy(t *testing.T) {
	t.Parallel()

	temporalClient := &temporalmocks.Client{}
	run := &temporalmocks.WorkflowRun{}
	run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		result, ok := args.Get(1).(*int)
		require.True(t, ok)
		*result = 99
	}).Return(nil).Once()
	workflowParams := OpenRouterSpendCapParams{
		OperationID: "admin_operation_placeholder", OrganizationID: "organization_placeholder",
		KeyType: string(openrouter.KeyTypeInternal), Limit: 100,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "admin_placeholder"), BypassPolicy: true,
	}
	temporalClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, workflowParams).Return(run, nil).Once()

	scheduler := &OpenRouterKeyRefresher{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	result, err := scheduler.SetAdminOpenRouterSpendCap(
		t.Context(), workflowParams.OperationID, workflowParams.OrganizationID, openrouter.KeyTypeInternal,
		workflowParams.Limit, workflowParams.Actor, nil,
	)
	require.NoError(t, err)
	require.Equal(t, 99, result)
	temporalClient.AssertExpectations(t)
	run.AssertExpectations(t)
}
