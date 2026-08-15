package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
)

func TestAccessPausedEmailWorkflowRetriesBeyondLegacyAttemptLimit(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, billingnotifications.SendAccessPausedInput) error {
			if attempts.Add(1) <= trialLifecycleEmailRetryMaximumAttempts {
				return errors.New("email service unavailable")
			}
			return nil
		},
		activity.RegisterOptions{Name: "SendAccessPausedEmail"},
	)

	env.ExecuteWorkflow(AccessPausedEmailWorkflow, billingnotifications.SendAccessPausedInput{
		EventID:        "<EVENT_ID>",
		OrganizationID: "<ORGANIZATION_ID>",
		Kind:           billingnotifications.AccessPausedSubscriptionLoss,
	})

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, trialLifecycleEmailRetryMaximumAttempts+1, attempts.Load())
}
