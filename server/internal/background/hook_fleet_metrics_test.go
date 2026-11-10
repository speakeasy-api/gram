package background

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestHookFleetMetricsWorkflowRetries(t *testing.T) {
	t.Parallel()
	for _, failures := range []int{0, hookFleetMetricsMaxAttempts - 1, hookFleetMetricsMaxAttempts} {
		t.Run(fmt.Sprintf("%d failures", failures), func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if calls <= failures {
					return errors.New("clickhouse unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: hookFleetMetricsActivity})
			env.ExecuteWorkflow(HookFleetMetricsWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if failures == hookFleetMetricsMaxAttempts {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, hookFleetMetricsMaxAttempts, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, failures+1, calls)
			}
		})
	}
}

func TestHookFleetMetricsSchedule(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	sc := &temporalmocks.ScheduleClient{}
	env := tenv.NewEnvironment(c, "test", sharedTaskQueue)
	c.On("ScheduleClient").Return(sc).Once()
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		require.Equal(t, "v1:hook-fleet-metrics:main", options.ID)
		require.Equal(t, []string{"0 6 * * *"}, options.Spec.CronExpressions)
		require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, options.Overlap)
		action, ok := options.Action.(*client.ScheduleWorkflowAction)
		require.True(t, ok)
		require.Equal(t, "main", action.TaskQueue)
		return true
	})).Return(nil, nil).Once()
	require.NoError(t, addHookFleetMetricsSchedule(t.Context(), env))
	c.AssertExpectations(t)
	sc.AssertExpectations(t)

	preview := &temporalmocks.Client{}
	require.NoError(t, addHookFleetMetricsSchedule(t.Context(), tenv.NewEnvironment(preview, "test", "preview-test")))
	preview.AssertNotCalled(t, "ScheduleClient")
}
