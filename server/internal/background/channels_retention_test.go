package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/channels"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestChannelsRetentionWorkflow(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			// Match other workflow tests: race instrumentation can delay the first yield.
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupChannelsHistory"})
			env.ExecuteWorkflow(ChannelsRetentionWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if fail {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, channelsRetentionMaxAttempts, calls, "a failing sweep must use its whole retry budget")
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, 1, calls, "a healthy sweep is one activity per tick")
			}
		})
	}
}

// TestChannelsRetentionRunTimeout pins the run timeout against the retry
// budget, so raising the attempt count or the activity timeout cannot leave a
// run that times out mid-retry.
func TestDrainChannelsRetention(t *testing.T) {
	t.Parallel()
	t.Run("stops as soon as the backlog is clear", func(t *testing.T) {
		t.Parallel()
		passes := 0
		require.NoError(t, drainChannelsRetention(t.Context(), func(context.Context) (channels.RetentionResult, error) {
			passes++
			return channels.RetentionResult{Organizations: 1, Messages: 3, Instances: 0, Truncated: false}, nil
		}))
		require.Equal(t, 1, passes)
	})

	t.Run("repeats while a pass reports it was truncated", func(t *testing.T) {
		t.Parallel()
		passes := 0
		require.NoError(t, drainChannelsRetention(t.Context(), func(context.Context) (channels.RetentionResult, error) {
			passes++
			return channels.RetentionResult{Organizations: 1, Messages: 500, Instances: 0, Truncated: passes < 4}, nil
		}))
		require.Equal(t, 4, passes)
	})

	t.Run("exhausting the budget is retryable, not silent", func(t *testing.T) {
		t.Parallel()
		passes := 0
		err := drainChannelsRetention(t.Context(), func(context.Context) (channels.RetentionResult, error) {
			passes++
			return channels.RetentionResult{Organizations: 1, Messages: 500, Instances: 0, Truncated: true}, nil
		})
		require.ErrorContains(t, err, "budget exhausted")
		require.Equal(t, channelsRetentionMaxPassesPerAttempt, passes, "the budget must bound one attempt")
	})

	t.Run("a failing pass stops immediately", func(t *testing.T) {
		t.Parallel()
		failure := errors.New("database unavailable")
		passes := 0
		err := drainChannelsRetention(t.Context(), func(context.Context) (channels.RetentionResult, error) {
			passes++
			return channels.RetentionResult{Organizations: 0, Messages: 0, Instances: 0, Truncated: false}, failure
		})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, passes)
	})
}

func TestChannelsRetentionRunTimeout(t *testing.T) {
	t.Parallel()
	// 3 attempts x 10m, plus 1m and 2m of backoff, plus 7m of margin.
	require.Equal(t, 40*time.Minute, channelsRetentionRunTimeout())
	require.Greater(t, channelsRetentionRunTimeout(), channelsRetentionMaxAttempts*channelsRetentionActivityTimeout)
	require.Less(t, channelsRetentionRunTimeout(), 2*channelsRetentionInterval,
		"a run must finish well inside its own interval so SKIP overlap does not starve the sweep")
}

// TestChannelsRetentionCostLine pins the published cost estimate: one start
// and one activity per hour per namespace, fixed rather than per organization.
func TestChannelsRetentionCostLine(t *testing.T) {
	t.Parallel()
	ticksPerMonth := int((30 * 24 * time.Hour) / channelsRetentionInterval)
	require.Equal(t, 720, ticksPerMonth)
	require.LessOrEqual(t, ticksPerMonth*2, 2000, "the hourly sweep must stay a four-figure monthly cost")
	require.GreaterOrEqual(t, channelsRetentionInterval, 5*time.Minute,
		"schedules under five minutes are not permitted")
}

func TestChannelsRetentionSchedule(t *testing.T) {
	t.Parallel()
	failure := errors.New("unavailable")
	for _, tc := range []struct {
		name                 string
		createErr, updateErr error
	}{
		{name: "create", createErr: nil, updateErr: nil},
		{name: "existing", createErr: temporal.ErrScheduleAlreadyRunning, updateErr: nil},
		{name: "create failure", createErr: failure, updateErr: nil},
		{name: "update failure", createErr: temporal.ErrScheduleAlreadyRunning, updateErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &temporalmocks.Client{}
			sc := &temporalmocks.ScheduleClient{}
			handle := &temporalmocks.ScheduleHandle{}
			env := tenv.NewEnvironment(c, "test", "channels-test")
			id := "v1:channels-retention:channels-test"
			c.On("ScheduleClient").Return(sc).Once()
			sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
				require.Equal(t, id, options.ID, "the schedule ID must name the task queue so a preview owns its own copy")
				require.Equal(t, channelsRetentionInterval-time.Second, options.CatchupWindow)
				require.Equal(t, channelsRetentionInterval, options.Spec.Intervals[0].Every)
				action, ok := options.Action.(*client.ScheduleWorkflowAction)
				require.True(t, ok)
				require.Equal(t, channelsRetentionRunTimeout(), action.WorkflowRunTimeout)
				require.Equal(t, "channels-test", action.TaskQueue)
				return true
			})).Return(nil, tc.createErr).Once()
			if errors.Is(tc.createErr, temporal.ErrScheduleAlreadyRunning) {
				sc.On("GetHandle", mock.Anything, id).Return(handle).Once()
				handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
					state := &client.ScheduleState{Paused: true, Note: "operator pause", LimitedActions: false, RemainingActions: 0}
					spec := &client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: channelsRetentionInterval, Offset: 0}}}
					update, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
						Action: &client.ScheduleWorkflowAction{TaskQueue: "channels-test", WorkflowRunTimeout: 35 * time.Minute},
						Policy: &client.SchedulePolicies{CatchupWindow: 24 * time.Hour},
						State:  state, Spec: spec,
					}}})
					require.NoError(t, err)
					action, ok := update.Schedule.Action.(*client.ScheduleWorkflowAction)
					require.True(t, ok)
					require.Equal(t, channelsRetentionRunTimeout(), action.WorkflowRunTimeout)
					require.Equal(t, channelsRetentionInterval-time.Second, update.Schedule.Policy.CatchupWindow)
					require.Equal(t, state, update.Schedule.State, "an operator pause must survive reconciliation")
					require.Equal(t, spec, update.Schedule.Spec)

					// A later startup must not rewrite an already reconciled schedule.
					unchanged := *update.Schedule
					update, err = options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: unchanged}})
					require.Nil(t, update)
					require.ErrorIs(t, err, temporal.ErrSkipScheduleUpdate)
					require.Equal(t, state, unchanged.State)

					// A preview worker must never steal the shared schedule.
					update, err = options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
						Action: &client.ScheduleWorkflowAction{TaskQueue: "other-queue"},
					}}})
					require.Nil(t, update)
					require.ErrorContains(t, err, "owned by another task queue")
					return true
				})).Return(tc.updateErr).Once()
			}
			err := AddChannelsRetentionSchedule(t.Context(), env)
			if errors.Is(tc.createErr, failure) || errors.Is(tc.updateErr, failure) {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			c.AssertExpectations(t)
			sc.AssertExpectations(t)
			handle.AssertExpectations(t)
		})
	}
}
