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
)

func TestEnterpriseTrialConversionKeyReconcileWorkflowRetriesCurrentStateProjection(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(func(_ context.Context, args activities.ReconcileEnterpriseTrialConversionKeysArgs) error {
		require.Equal(t, "organization_placeholder", args.OrganizationID)
		if attempts.Add(1) < 6 {
			return errors.New("upstream unavailable")
		}
		return nil
	}, activity.RegisterOptions{Name: "ReconcileEnterpriseTrialConversionKeys"})

	env.ExecuteWorkflow(EnterpriseTrialConversionKeyReconcileWorkflow, EnterpriseTrialConversionKeyReconcileParams{OrganizationID: "organization_placeholder"})
	require.NoError(t, env.GetWorkflowError())
	require.EqualValues(t, 6, attempts.Load())
}

func TestScheduleEnterpriseTrialConversionKeyReconciliationDedupesEventReplay(t *testing.T) {
	t.Parallel()
	temporalClient := &temporalmocks.Client{}
	options := mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
		return options.ID == "v1:openrouter-key-reconcile:enterprise-trial-conversion:event_placeholder" && options.TaskQueue == "test" && options.WorkflowRunTimeout == 0 && options.WorkflowIDReusePolicy == enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
	})
	params := EnterpriseTrialConversionKeyReconcileParams{OrganizationID: "organization_placeholder"}
	temporalClient.On("ExecuteWorkflow", mock.Anything, options, mock.Anything, params).Return(nil, nil).Once()
	temporalClient.On("ExecuteWorkflow", mock.Anything, options, mock.Anything, params).Return(nil, serviceerror.NewWorkflowExecutionAlreadyStarted("duplicate", "request_placeholder", "run_placeholder")).Once()

	scheduler := &OpenRouterKeyRefresher{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	require.NoError(t, scheduler.ScheduleEnterpriseTrialConversionKeyReconciliation(t.Context(), "event_placeholder", "organization_placeholder"))
	require.NoError(t, scheduler.ScheduleEnterpriseTrialConversionKeyReconciliation(t.Context(), "event_placeholder", "organization_placeholder"))
	temporalClient.AssertExpectations(t)
}
