package background

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
)

func TestScheduleCatchupPreservesOperatorState(t *testing.T) {
	t.Parallel()
	schedule := client.Schedule{
		Policy: &client.SchedulePolicies{Overlap: enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE, PauseOnFailure: true, CatchupWindow: 24 * time.Hour},
		State:  &client.ScheduleState{Paused: true, Note: "maintenance", LimitedActions: true, RemainingActions: 7},
	}
	setScheduleCatchup(&schedule, time.Minute)
	require.Equal(t, time.Minute, schedule.Policy.CatchupWindow)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE, schedule.Policy.Overlap)
	require.True(t, schedule.Policy.PauseOnFailure)
	require.True(t, schedule.State.Paused)
	require.Equal(t, "maintenance", schedule.State.Note)
	require.Equal(t, 7, schedule.State.RemainingActions)
}

func TestCreateScheduleWithCatchupMigratesOnlyPolicy(t *testing.T) {
	t.Parallel()
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	desired := client.ScheduleOptions{ID: "example", CatchupWindow: time.Hour, Action: &client.ScheduleWorkflowAction{TaskQueue: "queue"}}
	original := &client.ScheduleWorkflowAction{TaskQueue: "queue", ID: "preserved"}
	sc.On("Create", mock.Anything, desired).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, desired.ID).Return(handle).Once()
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		result, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
			Action: original, State: &client.ScheduleState{Paused: true, Note: "manual"},
		}}})
		require.NoError(t, err)
		require.Same(t, original, result.Schedule.Action)
		require.Equal(t, desired.CatchupWindow, result.Schedule.Policy.CatchupWindow)
		require.True(t, result.Schedule.State.Paused)
		return true
	})).Return(nil).Once()
	_, err := createScheduleWithCatchup(t.Context(), sc, desired)
	require.NoError(t, err)
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func TestCreateScheduleWithCatchupRejectsForeignQueue(t *testing.T) {
	t.Parallel()
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	desired := client.ScheduleOptions{ID: "example", CatchupWindow: time.Hour, Action: &client.ScheduleWorkflowAction{TaskQueue: "preview"}}
	sc.On("Create", mock.Anything, desired).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, desired.ID).Return(handle).Once()
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		result, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
			Action: &client.ScheduleWorkflowAction{TaskQueue: "primary"},
		}}})
		require.ErrorContains(t, err, "another task queue")
		require.Nil(t, result)
		return true
	})).Return(temporal.ErrScheduleAlreadyRunning).Once()
	_, err := createScheduleWithCatchup(t.Context(), sc, desired)
	require.Error(t, err)
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
}
