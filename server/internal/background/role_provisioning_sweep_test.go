package background

import (
	"context"
	"errors"
	"testing"
	"time"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestRoleProvisioningSweepWorkflowUsesOneBoundedActivity(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(ctx context.Context) error {
		calls++
		info := activity.GetInfo(ctx)
		require.Equal(t, 30*time.Second, info.StartToCloseTimeout)
		require.Equal(t, 2*time.Minute, info.ScheduleToCloseTimeout)
		return nil
	}, activity.RegisterOptions{Name: "RequestRoleProvisioningSweep"})
	env.ExecuteWorkflow(RoleProvisioningSweepWorkflow)
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
}

func TestRoleProvisioningSweepWorkflowBoundsRetries(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context) error { calls++; return errors.New("outbox unavailable") }, activity.RegisterOptions{Name: "RequestRoleProvisioningSweep"})
	env.ExecuteWorkflow(RoleProvisioningSweepWorkflow)
	require.ErrorContains(t, env.GetWorkflowError(), "outbox unavailable")
	require.Equal(t, 3, calls)
}

func TestRoleProvisioningSweepScheduleIsHourlyAndQueueScoped(t *testing.T) {
	t.Parallel()
	first := roleProvisioningSweepScheduleOptions("queue-a")
	second := roleProvisioningSweepScheduleOptions("queue-b")
	require.Equal(t, "v1:role-provisioning-sweep:queue-a", first.ID)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, first.Overlap)
	require.Equal(t, []client.ScheduleIntervalSpec{{Every: time.Hour}}, first.Spec.Intervals)
	require.Less(t, first.CatchupWindow, time.Hour)
	action, ok := first.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	require.Equal(t, "queue-a", action.TaskQueue)
	require.Equal(t, first.ID+"/scheduled", action.ID)
	require.Equal(t, 3*time.Minute, action.WorkflowExecutionTimeout)
}

func TestRoleProvisioningSweepSchedulePreservesOperatorState(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	env := tenv.NewEnvironment(c, "test-namespace", "queue-a")
	c.On("ScheduleClient").Return(sc).Once()
	sc.On("Create", mock.Anything, mock.Anything).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, roleProvisioningSweepScheduleID("queue-a")).Return(handle).Once()
	handle.On("Describe", mock.Anything).Return(&client.ScheduleDescription{Schedule: client.Schedule{Policy: &client.SchedulePolicies{CatchupWindow: 365 * 24 * time.Hour}}}, nil).Once()
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		update, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
			Action: &client.ScheduleWorkflowAction{TaskQueue: "queue-a"},
			Policy: &client.SchedulePolicies{CatchupWindow: 365 * 24 * time.Hour},
			State:  &client.ScheduleState{Paused: true, Note: "operator maintenance"},
		}}})
		require.NoError(t, err)
		require.True(t, update.Schedule.State.Paused)
		require.Equal(t, "operator maintenance", update.Schedule.State.Note)
		require.Equal(t, time.Hour-time.Second, update.Schedule.Policy.CatchupWindow)
		return true
	})).Return(nil).Once()
	require.NoError(t, addRoleProvisioningSweepSchedule(t.Context(), env))
	c.AssertExpectations(t)
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func TestRoleProvisioningSweepExistingScheduleIsNotUpdated(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	env := tenv.NewEnvironment(c, "test-namespace", "queue-a")
	c.On("ScheduleClient").Return(sc).Once()
	sc.On("Create", mock.Anything, mock.Anything).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, roleProvisioningSweepScheduleID("queue-a")).Return(handle).Once()
	handle.On("Describe", mock.Anything).Return(&client.ScheduleDescription{Schedule: client.Schedule{Policy: &client.SchedulePolicies{CatchupWindow: time.Hour - time.Second}}}, nil).Once()
	require.NoError(t, addRoleProvisioningSweepSchedule(t.Context(), env))
	c.AssertExpectations(t)
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
	handle.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}
