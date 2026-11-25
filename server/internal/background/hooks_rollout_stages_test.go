package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestHooksRolloutStagesWorkflowRetries(t *testing.T) {
	t.Parallel()
	for _, failures := range []int{0, hooksRolloutStagesMaxAttempts - 1, hooksRolloutStagesMaxAttempts} {
		t.Run(fmt.Sprintf("%d failures", failures), func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if calls <= failures {
					return errors.New("postgres unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: hooksRolloutStagesActivity})
			env.ExecuteWorkflow(HooksRolloutStagesWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if failures == hooksRolloutStagesMaxAttempts {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, hooksRolloutStagesMaxAttempts, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, failures+1, calls)
			}
		})
	}
}

func TestHooksRolloutStagesSchedule(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	sc := &temporalmocks.ScheduleClient{}
	env := tenv.NewEnvironment(c, "test", sharedTaskQueue)
	c.On("ScheduleClient").Return(sc).Once()
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		require.Equal(t, "v1:hooks-rollout-stages:main", options.ID)
		require.Equal(t, []client.ScheduleIntervalSpec{{Every: time.Hour}}, options.Spec.Intervals)
		require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, options.Overlap)
		action, ok := options.Action.(*client.ScheduleWorkflowAction)
		require.True(t, ok)
		require.Equal(t, "main", action.TaskQueue)
		return true
	})).Return(nil, nil).Once()
	require.NoError(t, addHooksRolloutStagesSchedule(t.Context(), env))
	c.AssertExpectations(t)
	sc.AssertExpectations(t)

	preview := &temporalmocks.Client{}
	require.NoError(t, addHooksRolloutStagesSchedule(t.Context(), tenv.NewEnvironment(preview, "test", "preview-test")))
	preview.AssertNotCalled(t, "ScheduleClient")
}

func TestRetryScheduleRegistration_RetriesUntilRegistered(t *testing.T) {
	t.Parallel()

	attempts := 0
	retryScheduleRegistration(t.Context(), slog.New(slog.DiscardHandler), "test", func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporal unavailable")
		}
		return nil
	}, time.Millisecond, 4*time.Millisecond)
	require.Equal(t, 3, attempts)
}

func TestRetryScheduleRegistration_StopsWhenContextEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	attempts := 0
	retryScheduleRegistration(ctx, slog.New(slog.DiscardHandler), "test", func(context.Context) error {
		attempts++
		cancel()
		return errors.New("temporal unavailable")
	}, time.Hour, time.Hour)
	require.Equal(t, 1, attempts)
}
