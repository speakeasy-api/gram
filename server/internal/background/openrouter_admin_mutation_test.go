package background

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const testOpenRouterAdminGuardDelay = 10 * time.Millisecond

func testOpenRouterAdminWorkflow(ctx workflow.Context, scope openrouterkeys.AdminReconciliationScope) error {
	return openRouterAdminReconciliationWorkflow(ctx, scope, testOpenRouterAdminGuardDelay)
}

func TestOpenRouterAdminCoordinatorStateAndIdentityArePrivacyBounded(t *testing.T) {
	t.Parallel()
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"}
	encoded, err := json.Marshal(openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: 42})
	require.NoError(t, err)
	require.JSONEq(t, `{"scope":{"organization_id":"organization_placeholder","key_type":"chat"},"cursor":42}`, string(encoded))
	require.Equal(t, "v1:openrouter-admin-reconcile:organization_placeholder:chat", OpenRouterAdminReconciliationWorkflowID(scope))
}

func TestTemporalOpenRouterAdminCoordinatorUsesAcknowledgedPayloadFreeUpdates(t *testing.T) {
	t.Parallel()
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"}
	workflowID := OpenRouterAdminReconciliationWorkflowID(scope)
	temporalClient := &temporalmocks.Client{}
	options := mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
		return options.ID == workflowID && options.TaskQueue == "test" && options.WorkflowIDReusePolicy == enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE && options.WorkflowIDConflictPolicy == enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
	})
	temporalClient.On("NewWithStartWorkflowOperation", options, mock.Anything, scope).Return((client.WithStartWorkflowOperation)(nil)).Twice()
	beginOptions := payloadFreeUpdateOptions(OpenRouterAdminBeginUpdate)
	completeOptions := payloadFreeUpdateOptions(OpenRouterAdminCompleteUpdate)
	temporalClient.On("UpdateWithStartWorkflow", mock.Anything, beginOptions).Return(&adminCoordinatorUpdateHandle{}, nil).Once()
	temporalClient.On("UpdateWithStartWorkflow", mock.Anything, completeOptions).Return(&adminCoordinatorUpdateHandle{}, nil).Once()
	coordinator := &TemporalOpenRouterAdminCoordinator{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	require.NoError(t, coordinator.Begin(t.Context(), scope))
	require.NoError(t, coordinator.CompleteAndWait(t.Context(), scope), "Complete acknowledges its own update instead of following an idle workflow run")
	temporalClient.AssertNotCalled(t, "SignalWithStartWorkflow", mock.Anything, workflowID, "complete", mock.Anything)
	temporalClient.AssertExpectations(t)
}

func TestTemporalOpenRouterAdminCoordinatorTimeoutIsNotSuccess(t *testing.T) {
	t.Parallel()
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"}
	temporalClient := &temporalmocks.Client{}
	temporalClient.On("NewWithStartWorkflowOperation", mock.Anything, mock.Anything, scope).Return((client.WithStartWorkflowOperation)(nil)).Once()
	temporalClient.On("UpdateWithStartWorkflow", mock.Anything, payloadFreeUpdateOptions(OpenRouterAdminCompleteUpdate)).Return(&adminCoordinatorUpdateHandle{err: context.DeadlineExceeded}, nil).Once()
	coordinator := &TemporalOpenRouterAdminCoordinator{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	require.ErrorIs(t, coordinator.CompleteAndWait(t.Context(), scope), context.DeadlineExceeded)
	temporalClient.AssertExpectations(t)
}

func payloadFreeUpdateOptions(name string) any {
	return mock.MatchedBy(func(options client.UpdateWithStartWorkflowOptions) bool {
		return options.StartWorkflowOperation == nil && options.UpdateOptions.UpdateName == name && options.UpdateOptions.WaitForStage == client.WorkflowUpdateStageAccepted && len(options.UpdateOptions.Args) == 0
	})
}

func TestOpenRouterAdminReconciliationRetriesTransientAndStopsPermanent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		permanent bool
		want      int32
	}{{"transient", false, 3}, {"permanent", true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			var cursor atomic.Int64
			var attempts atomic.Int32
			env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationScope) (int64, error) { return 0, nil }, activity.RegisterOptions{Name: OpenRouterAdminCaptureCursorActivityName})
			env.RegisterActivityWithOptions(func(context.Context, openrouterkeys.AdminReconciliationCheckpoint) (int64, error) {
				n := attempts.Add(1)
				if tc.permanent {
					return 0, temporal.NewNonRetryableApplicationError("permanent", OpenRouterAdminPermanentErrorType, nil)
				}
				if n < tc.want {
					return 0, errors.New("transient")
				}
				return cursor.Load(), nil
			}, activity.RegisterOptions{Name: OpenRouterAdminReconcileActivityName})
			armAdminMutation(t, env, "begin", time.Millisecond, func() { cursor.Store(1) })
			env.ExecuteWorkflow(testOpenRouterAdminWorkflow, openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"})
			if tc.permanent {
				require.Error(t, env.GetWorkflowError())
			} else {
				require.NoError(t, env.GetWorkflowError())
			}
			require.EqualValues(t, tc.want, attempts.Load())
		})
	}
}

type adminCoordinatorUpdateHandle struct{ err error }

func (*adminCoordinatorUpdateHandle) WorkflowID() string               { return "workflow" }
func (*adminCoordinatorUpdateHandle) RunID() string                    { return "run" }
func (*adminCoordinatorUpdateHandle) UpdateID() string                 { return "update" }
func (h *adminCoordinatorUpdateHandle) Get(context.Context, any) error { return h.err }
