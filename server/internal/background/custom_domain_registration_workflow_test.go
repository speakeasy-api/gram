package background

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func registrationWorkflowParams(customDomainID uuid.UUID) CustomDomainRegistrationParams {
	return CustomDomainRegistrationParams{
		OrgID:           "test-organization",
		Domain:          "apex-domain.example",
		CustomDomainID:  customDomainID,
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: k8s.ProvisionerKindIngress,
		IPAllowlist:     nil,
	}
}

func registerReconcileSignalActivity(t *testing.T, env *testsuite.TestWorkflowEnvironment, reconciles *int) {
	t.Helper()
	env.RegisterActivityWithOptions(
		func(_ context.Context, args SignalCustomDomainReconcileArgs) error {
			*reconciles++
			return nil
		},
		activity.RegisterOptions{Name: "SignalCustomDomainReconcile"},
	)
}

func TestCustomDomainRegistrationWorkflowPollsUntilVerified(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()

	verifyCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			require.Equal(t, customDomainID, args.CustomDomainID)
			verifyCalls++
			// DNS propagation completes on the third pass, hours later in
			// wall-clock terms; the workflow's timers absorb the wait.
			if verifyCalls < 3 {
				return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusDNSPending, Reason: "domain DNS records not found"}, nil
			}
			return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusVerified, Reason: ""}, nil
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	reconciles := 0
	registerReconcileSignalActivity(t, env, &reconciles)

	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, registrationWorkflowParams(customDomainID))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, verifyCalls)
	require.Equal(t, 1, reconciles, "reconciliation must run exactly once after verification")
}

func TestCustomDomainRegistrationWorkflowReverifySignalPreemptsTimer(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()

	verifyCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			verifyCalls++
			if verifyCalls < 3 {
				return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusDNSPending, Reason: "TXT record not found"}, nil
			}
			return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusVerified, Reason: ""}, nil
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	reconciles := 0
	registerReconcileSignalActivity(t, env, &reconciles)

	// Timeline without the signal: pass 1 at t=0, pass 2 at t=30s, pass 3 at
	// t=90s (60s backoff). A reverify at t=45s must cancel the live 60s timer
	// and run pass 3 immediately, finishing well before t=90s.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(CustomDomainReverifySignalName, nil)
	}, 45*time.Second)

	start := env.Now()
	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, registrationWorkflowParams(customDomainID))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, verifyCalls)
	require.Less(t, env.Now().Sub(start), 90*time.Second, "the reverify signal must preempt the live backoff timer")
	require.Equal(t, 1, reconciles)
}

func TestCustomDomainRegistrationWorkflowDrainsQueuedStartSignal(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()

	verifyCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			verifyCalls++
			if verifyCalls < 2 {
				return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusDNSPending, Reason: "TXT record not found"}, nil
			}
			return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusVerified, Reason: ""}, nil
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	reconciles := 0
	registerReconcileSignalActivity(t, env, &reconciles)

	// A signal queued at start (SignalWithStart always delivers one) must be
	// drained without disturbing the poll loop; wake-during-timer semantics
	// are proven separately by the 45s preemption test above.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(CustomDomainReverifySignalName, nil)
	}, 0)

	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, registrationWorkflowParams(customDomainID))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, verifyCalls)
	require.Equal(t, 1, reconciles)
}

func TestCustomDomainRegistrationWorkflowTimesOutAfterDeadline(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusDNSPending, Reason: "domain DNS records not found"}, nil
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	reconciles := 0
	registerReconcileSignalActivity(t, env, &reconciles)

	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, registrationWorkflowParams(customDomainID))

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom domain verification timed out")
	require.Equal(t, 0, reconciles, "an unverified domain must never reconcile")
}

func TestCustomDomainRegistrationWorkflowTerminalVerifyErrorFailsFast(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()

	verifyCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			verifyCalls++
			return activities.VerifyCustomDomainResult{}, temporal.NewNonRetryableApplicationError("custom domain no longer exists", "CustomDomainInvalid", nil)
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	reconciles := 0
	registerReconcileSignalActivity(t, env, &reconciles)

	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, registrationWorkflowParams(customDomainID))

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 1, verifyCalls, "a terminal error must not be retried by the poll loop")
	require.Equal(t, 0, reconciles)
}

func TestCustomDomainRegistrationWorkflowToleratesTransientVerifyErrors(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()

	verifyCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			verifyCalls++
			// A resolver/DB blip that exhausts one whole pass's activity
			// retries (3 attempts) must not kill the day-long wait; the loop
			// treats it as another pending tick and re-checks later.
			if verifyCalls <= 3 {
				return activities.VerifyCustomDomainResult{}, temporal.NewApplicationError("resolver unavailable", "Unavailable")
			}
			return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusVerified, Reason: ""}, nil
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	reconciles := 0
	registerReconcileSignalActivity(t, env, &reconciles)

	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, registrationWorkflowParams(customDomainID))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.GreaterOrEqual(t, verifyCalls, 2)
	require.Equal(t, 1, reconciles)
}
