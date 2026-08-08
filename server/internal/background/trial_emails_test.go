package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestTrialLifecycleEmailWorkflowRunTimeoutCoversRetryBudget(t *testing.T) {
	t.Parallel()

	require.Equal(t, 30*time.Minute, trialLifecycleEmailWorkflowRunTimeout)
}

func TestTrialLifecycleEmailWorkflowDispatchesAdminAdded(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var received TrialLifecycleEmailInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input TrialLifecycleEmailInput) error {
			received = input
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)

	input := TrialLifecycleEmailInput{
		Kind:           AdminAddedEmailKind,
		OrganizationID: "<ORGANIZATION_ID>",
		UserID:         "<USER_ID>",
	}
	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, input, received)
}

func TestTrialLifecycleEmailWorkflowRetriesActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error {
			if attempts.Add(1) < 3 {
				return errors.New("temporary Loops failure")
			}
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{
		Kind:           TrialStartedEmailKind,
		OrganizationID: "<ORGANIZATION_ID>",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, int32(3), attempts.Load())
}
