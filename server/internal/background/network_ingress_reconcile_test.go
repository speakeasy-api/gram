package background

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestNetworkIngressHealthTimeoutLeavesRefreshRunning(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	clientMock := &temporalmocks.Client{}
	run := &stubWorkflowRun{err: context.DeadlineExceeded, options: client.WorkflowRunGetOptions{DisableFollowingRuns: false}}
	clientMock.On("SignalWithStartWorkflow", mock.Anything,
		"v1:network-ingress-reconcile:authoritative:"+id.String(), "reconcile", "enqueue",
		mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			return options.TaskQueue == "authoritative" && options.StartDelay == 0 && options.WorkflowExecutionTimeout > 0
		}), mock.Anything, NetworkIngressReconcileParams{IngressID: id}).Return(run, nil).Once()
	refresher := NetworkIngressClient{Client: clientMock, Queue: "authoritative"}
	require.NoError(t, refresher.RefreshNetworkIngress(t.Context(), id))
	require.True(t, run.options.DisableFollowingRuns)
	clientMock.AssertExpectations(t)
	payload, err := json.Marshal(NetworkIngressReconcileParams{IngressID: id})
	require.NoError(t, err)
	require.JSONEq(t, `{"ingress_id":"`+id.String()+`"}`, string(payload))
}

func TestNetworkIngressWorkflowCoalescesSignalsDuringActivity(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	id := uuid.New()
	env.RegisterActivityWithOptions(func(context.Context, uuid.UUID) (NetworkIngressReconcileResult, error) {
		return NetworkIngressReconcileResult{Requeue: false}, nil
	}, activity.RegisterOptions{Name: NetworkIngressReconcileActivityName})
	env.OnActivity(NetworkIngressReconcileActivityName, mock.Anything, id).
		After(2*time.Second).Return(NetworkIngressReconcileResult{Requeue: false}, nil).Once()
	env.RegisterDelayedCallback(func() {
		for range 5 {
			env.SignalWorkflow("reconcile", "enqueue")
		}
	}, time.Second)
	env.ExecuteWorkflow(NetworkIngressReconcileWorkflow, NetworkIngressReconcileParams{IngressID: id})
	var continued *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continued)
	require.Equal(t, "NetworkIngressReconcileWorkflow", continued.WorkflowType.Name)
	env.AssertExpectations(t)
}

func TestNetworkIngressWorkflowUsesIDOnly(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	id := uuid.New()
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, actual uuid.UUID) (NetworkIngressReconcileResult, error) {
		require.Equal(t, id, actual)
		calls++
		return NetworkIngressReconcileResult{Requeue: false}, nil
	}, activity.RegisterOptions{Name: NetworkIngressReconcileActivityName})
	env.ExecuteWorkflow(NetworkIngressReconcileWorkflow, NetworkIngressReconcileParams{IngressID: id})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
}

func TestNetworkIngressWorkflowContinuesAsWrapperForChangedState(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, uuid.UUID) (NetworkIngressReconcileResult, error) {
		return NetworkIngressReconcileResult{Requeue: true}, nil
	}, activity.RegisterOptions{Name: NetworkIngressReconcileActivityName})
	env.ExecuteWorkflow(NetworkIngressReconcileWorkflow, NetworkIngressReconcileParams{IngressID: uuid.New()})
	var continued *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continued)
	require.Equal(t, "NetworkIngressReconcileWorkflow", continued.WorkflowType.Name)
}
