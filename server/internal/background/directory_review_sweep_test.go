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

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestDirectoryReviewSweepWorkflow(t *testing.T) {
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
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "SweepDirectoryReviews"})
			env.ExecuteWorkflow(DirectoryReviewSweepWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if fail {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, 3, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestDirectoryReviewSweepSchedule(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "already running"}[existing], func(t *testing.T) {
			t.Parallel()
			c := &temporalmocks.Client{}
			sc := &temporalmocks.ScheduleClient{}
			env := tenv.NewEnvironment(c, "test", "sweep-test")
			var createErr error
			if existing {
				createErr = temporal.ErrScheduleAlreadyRunning
			}
			c.On("ScheduleClient").Return(sc).Once()
			sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
				require.Equal(t, "v1:directory-review-sweep:sweep-test", options.ID)
				require.Equal(t, time.Hour, options.Spec.Intervals[0].Every)
				action, ok := options.Action.(*client.ScheduleWorkflowAction)
				require.True(t, ok)
				require.Equal(t, "sweep-test", action.TaskQueue)
				return true
			})).Return(nil, createErr).Once()

			require.NoError(t, AddDirectoryReviewSweepSchedule(t.Context(), env))
			c.AssertExpectations(t)
			sc.AssertExpectations(t)
		})
	}
}
