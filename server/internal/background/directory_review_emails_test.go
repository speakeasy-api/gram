package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

func TestDirectoryReviewEmailsWorkflowPassesRunTime(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	var got activities.SendDirectoryReviewEmailsArgs
	env.RegisterActivityWithOptions(func(_ context.Context, args activities.SendDirectoryReviewEmailsArgs) error {
		got = args
		return nil
	}, activity.RegisterOptions{Name: "SendDirectoryReviewEmails"})
	env.ExecuteWorkflow(DirectoryReviewEmailsWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.True(t, got.Now.Equal(start), "the activity decides with the workflow's time, so retries agree")
}

// Updating an existing schedule is covered with the other schedules by
// TestWorkers_RegisterSchedulesPreservesManualPauses.
func TestDirectoryReviewEmailsSchedule(t *testing.T) {
	t.Parallel()
	c := &temporalmocks.Client{}
	sc := &temporalmocks.ScheduleClient{}
	env := tenv.NewEnvironment(c, "test", "emails-test")
	c.On("ScheduleClient").Return(sc).Once()
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		require.Equal(t, "v1:directory-review-emails:emails-test", options.ID)
		require.Equal(t, 15*time.Minute, options.Spec.Intervals[0].Every)
		require.Equal(t, 15*time.Minute-time.Second, options.CatchupWindow)
		action, ok := options.Action.(*client.ScheduleWorkflowAction)
		require.True(t, ok)
		require.Equal(t, "emails-test", action.TaskQueue)
		require.Equal(t, directoryReviewEmailsRunTimeout, action.WorkflowRunTimeout)
		return true
	})).Return(nil, nil).Once()

	require.NoError(t, AddDirectoryReviewEmailsSchedule(t.Context(), env))
	c.AssertExpectations(t)
	sc.AssertExpectations(t)
}
