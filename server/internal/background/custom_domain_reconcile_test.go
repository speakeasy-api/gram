package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCustomDomainReconcileWorkflowID(t *testing.T) {
	t.Parallel()

	customDomainID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	require.Equal(t, "v1:custom-domain-reconcile:10000000-0000-0000-0000-000000000001", CustomDomainReconcileWorkflowID(customDomainID))
}

func TestCustomDomainReconcileWorkflowDrainsStartingSignal(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	customDomainID := uuid.New()
	params := CustomDomainReconcileParams{CustomDomainID: customDomainID}
	applies := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.ReconcileCustomDomainArgs) error {
			require.Equal(t, customDomainID, args.CustomDomainID)
			applies++
			return nil
		},
		activity.RegisterOptions{Name: "ReconcileCustomDomain"},
	)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(customDomainReconcileSignal(params), "reconcile")
	}, 0)

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, applies)
}

func TestCustomDomainReconcileWorkflowCoalescesSignalsDuringApply(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := CustomDomainReconcileParams{CustomDomainID: uuid.New()}
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReconcileCustomDomainArgs) error {
			env.SignalWorkflow(customDomainReconcileSignal(params), "reconcile")
			env.SignalWorkflow(customDomainReconcileSignal(params), "reconcile")
			env.SignalWorkflow(customDomainReconcileSignal(params), "reconcile")
			return nil
		},
		activity.RegisterOptions{Name: "ReconcileCustomDomain"},
	)

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, "CustomDomainReconcileWorkflow", continueAsNewErr.WorkflowType.Name)
}

func TestCustomDomainReconcileWorkflowRunsAgainForSignalAfterApply(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	params := CustomDomainReconcileParams{CustomDomainID: uuid.New()}
	signaled := false
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.ReconcileCustomDomainArgs) error {
			return nil
		},
		activity.RegisterOptions{Name: "ReconcileCustomDomain"},
	)
	env.SetOnActivityCompletedListener(func(_ *activity.Info, _ converter.EncodedValue, _ error) {
		if signaled {
			return
		}
		signaled = true
		env.SignalWorkflow(customDomainReconcileSignal(params), "reconcile")
	})

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, params)

	require.True(t, env.IsWorkflowCompleted())
	var continueAsNewErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueAsNewErr)
	require.Equal(t, "CustomDomainReconcileWorkflow", continueAsNewErr.WorkflowType.Name)
}

func TestCustomDomainDeletionWorkflowRetainsLegacyActivityPath(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var received activities.CustomDomainIngressArgs
	env.RegisterActivityWithOptions(
		func(_ context.Context, args activities.CustomDomainIngressArgs) error {
			received = args
			return nil
		},
		activity.RegisterOptions{Name: "CustomDomainIngress"},
	)

	env.ExecuteWorkflow(CustomDomainDeletionWorkflow, CustomDomainDeletionParams{
		OrgID:           "test-organization",
		Domain:          "legacy-delete.example.com",
		IngressName:     "legacy-resource",
		CertSecretName:  "legacy-secret",
		ProvisionerKind: k8s.ProvisionerKindIngress,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, activities.CustomDomainIngressActionDelete, received.Action)
	require.Equal(t, "legacy-resource", received.IngressName)
	require.Equal(t, "legacy-secret", received.CertSecretName)
}

func TestCustomDomainRegistrationWorkflowUsesReconcileBridgeBudget(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(
		func(context.Context, activities.VerifyCustomDomainArgs) (activities.VerifyCustomDomainResult, error) {
			return activities.VerifyCustomDomainResult{Status: activities.VerifyStatusVerified, Reason: ""}, nil
		},
		activity.RegisterOptions{Name: "VerifyCustomDomainV2"},
	)
	bridgeAttempts := 0
	var bridgeTimeout time.Duration
	env.RegisterActivityWithOptions(
		func(ctx context.Context, _ SignalCustomDomainReconcileArgs) error {
			bridgeAttempts++
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			bridgeTimeout = time.Until(deadline)
			return errors.New("bridge failed")
		},
		activity.RegisterOptions{Name: "SignalCustomDomainReconcile"},
	)

	env.ExecuteWorkflow(CustomDomainRegistrationWorkflow, CustomDomainRegistrationParams{
		OrgID:           "test-organization",
		Domain:          "test.example.com",
		CreatedBy:       urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"),
		CreatedByName:   nil,
		ProvisionerKind: k8s.ProvisionerKindIngress,
		IPAllowlist:     nil,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, signalCustomDomainReconcileActivityMaximumAttempts, bridgeAttempts)
	require.Greater(t, bridgeTimeout, 11*time.Minute)
	require.LessOrEqual(t, bridgeTimeout, signalCustomDomainReconcileStartToCloseTimeout)
}
