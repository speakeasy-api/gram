package background

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestSlackDirectoryWorkflowRetriesOneSmallInput(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	started := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(started)
	input := slackdirectoryconnections.SyncInput{OrganizationID: "org_example", ConnectionID: uuid.New(), Generation: uuid.New(), ActorID: "user_example", StartedAt: time.Time{}}
	attempts := 0
	env.RegisterActivityWithOptions(func(_ context.Context, received slackdirectoryconnections.SyncInput) error {
		require.Equal(t, started, received.StartedAt)
		require.Equal(t, input.Generation, received.Generation)
		attempts++
		if attempts == 1 {
			return temporal.NewApplicationError("sync_busy", "SlackDirectorySync")
		}
		return nil
	}, activity.RegisterOptions{Name: "SyncSlackDirectory"})
	env.ExecuteWorkflow(SlackDirectorySyncWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, attempts)
}

func TestSlackDirectorySchedulerScopesStartAndStateToEnvironment(t *testing.T) {
	t.Parallel()
	input := slackdirectoryconnections.SyncInput{OrganizationID: "org_example", ConnectionID: uuid.New(), Generation: uuid.New(), ActorID: "user_example", StartedAt: time.Time{}}
	seen := make(map[string]bool)
	for _, queue := range []tenv.TaskQueueName{"preview-one", "preview-two"} {
		for _, namespace := range []tenv.NamespaceName{"dev", "prod"} {
			// Each Environment owns the client dialed for its namespace. Both operations
			// must use that client and the queue-scoped ID even when DB IDs are identical.
			mockClient := new(mocks.Client)
			expectedID := slackDirectoryWorkflowID(queue, input.ConnectionID, input.Generation)
			env := tenv.NewEnvironment(mockClient, namespace, queue)
			scheduler := NewSlackDirectorySyncScheduler(env)
			mockClient.On("ExecuteWorkflow", mock.Anything, mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
				return options.ID == expectedID && options.TaskQueue == string(queue) && options.WorkflowIDConflictPolicy == enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING && options.WorkflowIDReusePolicy == enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE
			}), mock.Anything, input).Return(new(mocks.WorkflowRun), nil).Once()
			mockClient.On("DescribeWorkflowExecution", mock.Anything, expectedID, "").Return(nil, serviceerror.NewNotFound("missing")).Once()
			require.NoError(t, scheduler.Start(t.Context(), input))
			state, err := scheduler.State(t.Context(), input.ConnectionID, input.Generation)
			require.NoError(t, err)
			require.Equal(t, "idle", state.Status)
			mockClient.AssertExpectations(t)
			seen[expectedID] = true
		}
	}
	require.Len(t, seen, 2)
}

func TestSlackDirectoryWorkflowDoesNotRetryAuthorizationFailure(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, slackdirectoryconnections.SyncInput) error {
		attempts++
		return temporal.NewNonRetryableApplicationError("authorization_expired", "SlackDirectorySync", nil)
	}, activity.RegisterOptions{Name: "SyncSlackDirectory"})
	env.ExecuteWorkflow(SlackDirectorySyncWorkflow, slackdirectoryconnections.SyncInput{OrganizationID: "org_example", ConnectionID: uuid.New(), Generation: uuid.New(), ActorID: "user_example", StartedAt: time.Time{}})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 1, attempts)
}
