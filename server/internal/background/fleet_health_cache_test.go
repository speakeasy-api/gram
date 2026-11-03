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

func TestFleetHealthCacheWorkflow(t *testing.T) {
	t.Parallel()
	for _, failures := range []int{0, 1, 3} {
		t.Run(string(rune('0'+failures)), func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if calls <= failures {
					return errors.New("cache unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: fleetHealthCacheActivity})
			env.ExecuteWorkflow(FleetHealthCacheWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if failures == 3 {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, 3, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, failures+1, calls)
			}
		})
	}
}

func TestFleetHealthCacheSchedule(t *testing.T) {
	t.Parallel()
	for _, createErr := range []error{nil, temporal.ErrScheduleAlreadyRunning, errors.New("unavailable")} {
		name := "created"
		if createErr != nil {
			name = createErr.Error()
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := &temporalmocks.Client{}
			sc := &temporalmocks.ScheduleClient{}
			env := tenv.NewEnvironment(c, "test", sharedTaskQueue)
			c.On("ScheduleClient").Return(sc).Once()
			sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
				require.Equal(t, "v1:fleet-health-cache:main", options.ID)
				require.Equal(t, []string{"0 2 * * *"}, options.Spec.CronExpressions)
				require.Equal(t, "UTC", options.Spec.TimeZoneName)
				require.True(t, options.TriggerImmediately)
				require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, options.Overlap)
				require.Equal(t, fleetHealthCacheCatchupWindow, options.CatchupWindow)
				action, ok := options.Action.(*client.ScheduleWorkflowAction)
				require.True(t, ok)
				require.Equal(t, "main", action.TaskQueue)
				require.Equal(t, fleetHealthCacheRunTimeout, action.WorkflowRunTimeout)
				return true
			})).Return(nil, createErr).Once()
			err := addFleetHealthCacheSchedule(t.Context(), env)
			if createErr != nil && !errors.Is(createErr, temporal.ErrScheduleAlreadyRunning) {
				require.ErrorIs(t, err, createErr)
			} else {
				require.NoError(t, err)
			}
			c.AssertExpectations(t)
			sc.AssertExpectations(t)
		})
	}
}

func TestFleetHealthCacheScheduleSkipsPreviews(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	env := tenv.NewEnvironment(c, "test", "preview-test")
	require.NoError(t, addFleetHealthCacheSchedule(t.Context(), env))
	c.AssertNotCalled(t, "ScheduleClient")
}
