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
	start := &adminCoordinatorStartOperation{}
	temporalClient.On("NewWithStartWorkflowOperation", options, mock.Anything, scope).Return(start).Twice()
	const token = int64(42)
	beginOptions := updateWithStartOptions(OpenRouterAdminBeginUpdate, start)
	completeOptions := updateWithStartOptions(OpenRouterAdminCompleteUpdate, start, token)
	temporalClient.On("UpdateWithStartWorkflow", mock.Anything, beginOptions).Return(&adminCoordinatorUpdateHandle{result: token}, nil).Once()
	temporalClient.On("UpdateWithStartWorkflow", mock.Anything, completeOptions).Return(&adminCoordinatorUpdateHandle{}, nil).Once()
	coordinator := &TemporalOpenRouterAdminCoordinator{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	gotToken, err := coordinator.Begin(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, token, gotToken)
	require.NoError(t, coordinator.CompleteAndWait(t.Context(), scope, gotToken), "Complete acknowledges its own update instead of following an idle workflow run")
	temporalClient.AssertNotCalled(t, "SignalWithStartWorkflow", mock.Anything, workflowID, "complete", mock.Anything)
	temporalClient.AssertExpectations(t)
}

func TestTemporalOpenRouterAdminCoordinatorTimeoutIsNotSuccess(t *testing.T) {
	t.Parallel()
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: "organization_placeholder", KeyType: "chat"}
	temporalClient := &temporalmocks.Client{}
	start := &adminCoordinatorStartOperation{}
	temporalClient.On("NewWithStartWorkflowOperation", mock.Anything, mock.Anything, scope).Return(start).Once()
	temporalClient.On("UpdateWithStartWorkflow", mock.Anything, updateWithStartOptions(OpenRouterAdminCompleteUpdate, start, int64(42))).Return(&adminCoordinatorUpdateHandle{err: context.DeadlineExceeded}, nil).Once()
	coordinator := &TemporalOpenRouterAdminCoordinator{TemporalEnv: tenv.NewEnvironment(temporalClient, "test", "test")}
	require.ErrorIs(t, coordinator.CompleteAndWait(t.Context(), scope, 42), context.DeadlineExceeded)
	temporalClient.AssertExpectations(t)
}

func updateWithStartOptions(name string, start client.WithStartWorkflowOperation, args ...any) any {
	return mock.MatchedBy(func(options client.UpdateWithStartWorkflowOptions) bool {
		if start == nil || options.StartWorkflowOperation != start || options.UpdateOptions.UpdateName != name || options.UpdateOptions.WaitForStage != client.WorkflowUpdateStageAccepted || len(options.UpdateOptions.Args) != len(args) {
			return false
		}
		for i := range args {
			if options.UpdateOptions.Args[i] != args[i] {
				return false
			}
		}
		return true
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
			t.Parallel()
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
			require.Equal(t, tc.want, attempts.Load())
		})
	}
}

type adminCoordinatorStartOperation struct{}

func (*adminCoordinatorStartOperation) Get(context.Context) (client.WorkflowRun, error) {
	return nil, nil
}

type adminCoordinatorUpdateHandle struct {
	err    error
	result int64
}

func (*adminCoordinatorUpdateHandle) WorkflowID() string { return "workflow" }
func (*adminCoordinatorUpdateHandle) RunID() string      { return "run" }
func (*adminCoordinatorUpdateHandle) UpdateID() string   { return "update" }
func (h *adminCoordinatorUpdateHandle) Get(_ context.Context, value any) error {
	if result, ok := value.(*int64); ok {
		*result = h.result
	}
	return h.err
}
