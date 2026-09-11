package background

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestNetworkIngressSweepWorkflowUsesCombinedActivityRetryBudgets(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var activityNames []string
	for _, name := range []string{"SweepNetworkIngresses", "FindNetworkIngressOrphans"} {
		env.RegisterActivityWithOptions(func(ctx context.Context) error {
			info := activity.GetInfo(ctx)
			require.Equal(t, networkIngressSweepActivityTimeout, info.StartToCloseTimeout)
			require.Equal(t, networkIngressSweepActivityRetryBudget, info.ScheduleToCloseTimeout)
			activityNames = append(activityNames, info.ActivityType.Name)
			return nil
		}, activity.RegisterOptions{Name: name})
	}

	env.ExecuteWorkflow(NetworkIngressSweepWorkflow)

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{"SweepNetworkIngresses", "FindNetworkIngressOrphans"}, activityNames)
	require.Equal(t, 2*networkIngressSweepActivityRetryBudget, networkIngressSweepExecutionTimeout)
}

func TestNetworkIngressSweepScheduleIsStableAcrossQueueChanges(t *testing.T) {
	t.Parallel()

	first := buildNetworkIngressSweepScheduleOptions("gram-dev", "queue-a")
	second := buildNetworkIngressSweepScheduleOptions("gram-dev", "queue-b")
	otherNamespace := buildNetworkIngressSweepScheduleOptions("gram-prod", "queue-b")
	firstAction := requireNetworkIngressSweepAction(t, first.Action)
	secondAction := requireNetworkIngressSweepAction(t, second.Action)

	require.Equal(t, "v1:network-ingress-reconcile-sweep:gram-dev", first.ID)
	require.Equal(t, first.ID, second.ID)
	require.NotEqual(t, first.ID, otherNamespace.ID)
	require.Equal(t, firstAction.ID, secondAction.ID)
	require.Equal(t, first.ID+"/scheduled", secondAction.ID)
	require.Equal(t, "queue-a", firstAction.TaskQueue)
	require.Equal(t, "queue-b", secondAction.TaskQueue)
	require.Equal(t, networkIngressSweepExecutionTimeout, secondAction.WorkflowExecutionTimeout)
}

func TestAddNetworkIngressSweepUpdatesExistingSchedule(t *testing.T) {
	t.Parallel()

	clientMock := &temporalmocks.Client{}
	scheduleClient := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	env := tenv.NewEnvironment(clientMock, "gram-dev", "new-queue")
	desired := buildNetworkIngressSweepScheduleOptions("gram-dev", "new-queue")

	clientMock.On("ScheduleClient").Return(scheduleClient).Once()
	scheduleClient.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		return options.ID == desired.ID && requireNetworkIngressSweepAction(t, options.Action).TaskQueue == "new-queue"
	})).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	scheduleClient.On("GetHandle", mock.Anything, desired.ID).Return(handle).Once()
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		update, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{
			Schedule: client.Schedule{
				Spec:   &client.ScheduleSpec{CronExpressions: []string{"stale"}},
				Action: &client.ScheduleWorkflowAction{ID: "stale", TaskQueue: "new-queue"},
			},
		}})
		require.NoError(t, err)
		require.Equal(t, desired.Spec, *update.Schedule.Spec)
		desiredAction := requireNetworkIngressSweepAction(t, desired.Action)
		updatedAction := requireNetworkIngressSweepAction(t, update.Schedule.Action)
		require.Equal(t, desiredAction.ID, updatedAction.ID)
		require.Equal(t, desiredAction.TaskQueue, updatedAction.TaskQueue)
		require.Equal(t, desiredAction.WorkflowExecutionTimeout, updatedAction.WorkflowExecutionTimeout)
		require.NotNil(t, update.Schedule.Policy)
		require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, update.Schedule.Policy.Overlap)
		return true
	})).Return(nil).Once()

	require.NoError(t, addNetworkIngressSweep(t.Context(), env))
	clientMock.AssertExpectations(t)
	scheduleClient.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func TestAddNetworkIngressSweepRefusesAnotherQueueOwner(t *testing.T) {
	t.Parallel()

	clientMock := &temporalmocks.Client{}
	scheduleClient := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	env := tenv.NewEnvironment(clientMock, "gram-dev", "new-queue")
	desired := buildNetworkIngressSweepScheduleOptions("gram-dev", "new-queue")

	clientMock.On("ScheduleClient").Return(scheduleClient).Once()
	scheduleClient.On("Create", mock.Anything, mock.Anything).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	scheduleClient.On("GetHandle", mock.Anything, desired.ID).Return(handle).Once()
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		update, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{
			Schedule: client.Schedule{Action: &client.ScheduleWorkflowAction{ID: "existing", TaskQueue: "authoritative-queue"}},
		}})
		require.Nil(t, update)
		require.ErrorContains(t, err, "owned by another task queue")
		return true
	})).Return(errors.New("network ingress sweep is owned by another task queue")).Once()

	require.ErrorContains(t, addNetworkIngressSweep(t.Context(), env), "owned by another task queue")
	clientMock.AssertExpectations(t)
	scheduleClient.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func requireNetworkIngressSweepAction(t *testing.T, action client.ScheduleAction) *client.ScheduleWorkflowAction {
	t.Helper()
	workflowAction, ok := action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	return workflowAction
}
