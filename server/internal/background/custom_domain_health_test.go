package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/k8s"
)

func TestScheduledCustomDomainHealthCheckWorkflowID(t *testing.T) {
	t.Parallel()

	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	require.Equal(
		t,
		"v1:custom-domain-health:2026-07-21:019c1a55-c04e-7b95-a840-b5054b457e27",
		scheduledCustomDomainHealthCheckWorkflowID("2026-07-21", domainID),
	)
}

func TestManualCustomDomainHealthCheckWorkflowIDIsUnique(t *testing.T) {
	t.Parallel()

	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	first := manualCustomDomainHealthCheckWorkflowID(domainID)
	second := manualCustomDomainHealthCheckWorkflowID(domainID)

	require.NotEqual(t, first, second)
	require.Contains(t, first, "v1:custom-domain-health:manual:"+domainID.String()+":")
}

func TestCustomDomainNotifyWorkflowID(t *testing.T) {
	t.Parallel()

	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	checkedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	require.Equal(
		t,
		"v1:custom-domain-unhealthy-notify:019c1a55-c04e-7b95-a840-b5054b457e27:1784635200000000",
		customDomainNotifyWorkflowID(domainID, checkedAt),
	)
}

func TestCustomDomainAutoDisableWorkflowID(t *testing.T) {
	t.Parallel()

	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	checkedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	require.Equal(
		t,
		"v1:custom-domain-auto-disable:019c1a55-c04e-7b95-a840-b5054b457e27:1784635200000000",
		customDomainAutoDisableWorkflowID(domainID, checkedAt),
	)
}

func TestCustomDomainHealthCheckWorkflowDispatchesDetachedNotification(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")

	healthCheckCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.CheckCustomDomainHealthArgs) (activities.CustomDomainHealthCheckResult, error) {
			healthCheckCalls++
			return activities.CustomDomainHealthCheckResult{
				UnhealthyNotification: activities.NotifyCustomDomainUnhealthyArgs{
					CustomDomainID: input.CustomDomainID,
					OrganizationID: input.OrganizationID,
					Domain:         "unhealthy.example.com",
					Issue:          customdomains.HealthIssueDNSNotFound,
					CheckedAt:      input.CheckedAt,
				},
				AutoDisable: activities.AutoDisableCustomDomainArgs{
					CustomDomainID:  uuid.Nil,
					OrganizationID:  "",
					Domain:          "",
					Issue:           "",
					CheckedAt:       time.Time{},
					IngressName:     "",
					CertSecretName:  "",
					ProvisionerKind: "",
				},
			}, nil
		},
		activity.RegisterOptions{Name: "CheckCustomDomainHealth"},
	)

	env.RegisterWorkflow(CustomDomainUnhealthyNotifyWorkflow)
	env.OnWorkflow(CustomDomainUnhealthyNotifyWorkflow, mock.Anything, mock.MatchedBy(func(args activities.NotifyCustomDomainUnhealthyArgs) bool {
		return args.CustomDomainID == domainID &&
			args.OrganizationID == "test-organization" &&
			args.Domain == "unhealthy.example.com" &&
			args.Issue == customdomains.HealthIssueDNSNotFound &&
			!args.CheckedAt.IsZero()
	})).Return(nil).Once()

	env.ExecuteWorkflow(CustomDomainHealthCheckWorkflow, CustomDomainHealthCheckParams{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		AutoDisable:    false,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, healthCheckCalls)
	env.AssertExpectations(t)
}

func TestScheduledCustomDomainHealthCheckDispatchesAutoDisable(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	autoDisable := activities.AutoDisableCustomDomainArgs{
		CustomDomainID:  domainID,
		OrganizationID:  "test-organization",
		Domain:          "disabled.example.com",
		Issue:           customdomains.HealthIssueDNSNotFound,
		CheckedAt:       time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		IngressName:     "disabled-example-com",
		CertSecretName:  "disabled-example-com-tls",
		ProvisionerKind: k8s.ProvisionerKindIngress,
	}

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CheckCustomDomainHealthArgs) (activities.CustomDomainHealthCheckResult, error) {
			return activities.CustomDomainHealthCheckResult{
				UnhealthyNotification: activities.NotifyCustomDomainUnhealthyArgs{
					CustomDomainID: uuid.Nil,
					OrganizationID: "",
					Domain:         "",
					Issue:          "",
					CheckedAt:      time.Time{},
				},
				AutoDisable: autoDisable,
			}, nil
		},
		activity.RegisterOptions{Name: "CheckCustomDomainHealth"},
	)
	env.RegisterWorkflow(CustomDomainAutoDisableWorkflow)
	env.OnWorkflow(CustomDomainAutoDisableWorkflow, mock.Anything, autoDisable).Return(nil).Once()

	env.ExecuteWorkflow(CustomDomainHealthCheckWorkflow, CustomDomainHealthCheckParams{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		AutoDisable:    true,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestCustomDomainUnhealthyNotifyWorkflowRetriesDelivery(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	checkedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	notificationCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.NotifyCustomDomainUnhealthyArgs) error {
			notificationCalls++
			require.Equal(t, domainID, input.CustomDomainID)
			require.Equal(t, "test-organization", input.OrganizationID)
			require.Equal(t, "unhealthy.example.com", input.Domain)
			require.Equal(t, customdomains.HealthIssueDNSNotFound, input.Issue)
			if notificationCalls == 1 {
				return errors.New("temporary email transport failure")
			}
			return nil
		},
		activity.RegisterOptions{Name: "NotifyCustomDomainUnhealthy"},
	)

	env.ExecuteWorkflow(CustomDomainUnhealthyNotifyWorkflow, activities.NotifyCustomDomainUnhealthyArgs{
		CustomDomainID: domainID,
		OrganizationID: "test-organization",
		Domain:         "unhealthy.example.com",
		Issue:          customdomains.HealthIssueDNSNotFound,
		CheckedAt:      checkedAt,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, notificationCalls)
}

func TestCustomDomainAutoDisableWorkflowDeletesDeactivatesAndNotifies(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	args := activities.AutoDisableCustomDomainArgs{
		CustomDomainID:  uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27"),
		OrganizationID:  "test-organization",
		Domain:          "disabled.example.com",
		Issue:           customdomains.HealthIssueDNSNotFound,
		CheckedAt:       time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		IngressName:     "disabled-example-com",
		CertSecretName:  "disabled-example-com-tls",
		ProvisionerKind: k8s.ProvisionerKindIngress,
	}

	var calls []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.CustomDomainIngressArgs) error {
			require.Equal(t, activities.CustomDomainIngressActionDelete, input.Action)
			require.Equal(t, args.IngressName, input.ResourceName)
			calls = append(calls, "delete")
			return nil
		},
		activity.RegisterOptions{Name: "CustomDomainIngress"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.AutoDisableCustomDomainArgs) (bool, error) {
			require.Equal(t, args, input)
			calls = append(calls, "deactivate")
			return true, nil
		},
		activity.RegisterOptions{Name: "DeactivateUnhealthyCustomDomain"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.AutoDisableCustomDomainArgs) error {
			require.Equal(t, args, input)
			calls = append(calls, "notify")
			return nil
		},
		activity.RegisterOptions{Name: "NotifyCustomDomainDisabled"},
	)

	env.ExecuteWorkflow(CustomDomainAutoDisableWorkflow, args)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{"delete", "deactivate", "notify"}, calls)
}
