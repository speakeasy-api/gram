package background

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

// mockActivities provides mock implementations for workflow testing
type mockActivities struct {
	mock.Mock
}

func (m *mockActivities) ListActiveCustomDomains(_ context.Context, args activities.ListActiveCustomDomainsArgs) ([]string, error) {
	ret := m.Called(args)
	if ret.Get(0) == nil {
		return nil, ret.Error(1) //nolint:wrapcheck // test mock
	}
	result, ok := ret.Get(0).([]string)
	if !ok {
		return nil, ret.Error(1) //nolint:wrapcheck // test mock
	}
	return result, ret.Error(1) //nolint:wrapcheck // test mock
}

func (m *mockActivities) EnsureCustomDomainIngress(_ context.Context, args activities.EnsureCustomDomainIngressArgs) error {
	return m.Called(args).Error(0) //nolint:wrapcheck // test mock
}

func TestCustomDomainReconcileWorkflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		domains       []string
		failedDomains map[string]bool
	}{
		{
			name:    "no domains",
			domains: []string{},
		},
		{
			name:    "single domain success",
			domains: []string{"example.com"},
		},
		{
			name:    "multiple domains all succeed",
			domains: []string{"a.com", "b.com", "c.com"},
		},
		{
			name:          "partial failure",
			domains:       []string{"a.com", "b.com", "c.com"},
			failedDomains: map[string]bool{"b.com": true},
		},
		{
			name:          "all fail",
			domains:       []string{"a.com", "b.com"},
			failedDomains: map[string]bool{"a.com": true, "b.com": true},
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestWorkflowEnvironment()

			ma := &mockActivities{}

			env.RegisterActivity(ma.ListActiveCustomDomains)
			env.RegisterActivity(ma.EnsureCustomDomainIngress)

			ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
				Return(tt.domains, nil).Once()

			for _, domain := range tt.domains {
				if tt.failedDomains != nil && tt.failedDomains[domain] {
					ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: domain}).
						Return(errors.New("simulated failure")).Once()
				} else {
					ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: domain}).
						Return(nil).Once()
				}
			}

			env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
				OrganizationID:    "",
				ContinuationState: nil,
			})

			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())

			ma.AssertExpectations(t)
		})
	}
}

func TestCustomDomainReconcileWorkflow_ContinueAsNew(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	// Generate 150 domains to trigger ContinueAsNew at 100
	domains := make([]string, 150)
	for i := range domains {
		domains[i] = fmt.Sprintf("domain%03d.com", i)
	}

	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
		Return(domains, nil).Once()

	// Only first 100 domains should be processed before ContinueAsNew
	for i := 0; i < 100; i++ {
		ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: domains[i]}).
			Return(nil).Once()
	}

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID:    "",
		ContinuationState: nil,
	})

	require.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	require.Error(t, err, "should trigger ContinueAsNew")

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, err, &continueErr, "error should be ContinueAsNewError")

	ma.AssertExpectations(t)
}

func TestCustomDomainReconcileWorkflow_ExactBoundary(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	// Exactly 100 domains should NOT trigger ContinueAsNew (no remaining work)
	domains := make([]string, 100)
	for i := range domains {
		domains[i] = fmt.Sprintf("domain%03d.com", i)
	}

	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
		Return(domains, nil).Once()

	for i := 0; i < 100; i++ {
		ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: domains[i]}).
			Return(nil).Once()
	}

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID:    "",
		ContinuationState: nil,
	})

	require.True(t, env.IsWorkflowCompleted())

	// Should complete normally, NOT ContinueAsNew
	err := env.GetWorkflowError()
	require.NoError(t, err, "exactly 100 domains should complete without ContinueAsNew")

	ma.AssertExpectations(t)
}

func TestCustomDomainReconcileWorkflow_ContinuationState(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	domains := []string{"a.com", "b.com", "c.com", "d.com", "e.com"}
	alreadyProcessed := []string{"a.com", "b.com", "c.com"}

	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
		Return(domains, nil).Once()

	// Only d.com and e.com should be processed (a,b,c already done)
	ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: "d.com"}).Return(nil).Once()
	ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: "e.com"}).Return(nil).Once()

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID: "",
		ContinuationState: &ReconcileContinuationState{
			ProcessedDomains: alreadyProcessed,
			Reconciled:       3,
			Failed:           0,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	ma.AssertExpectations(t)
}

func TestCustomDomainReconcileWorkflow_OrganizationID(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	orgID := "org_123456"
	domains := []string{"custom.example.com"}

	// Verify OrganizationID is passed to activity
	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: orgID}).
		Return(domains, nil).Once()

	ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: "custom.example.com"}).
		Return(nil).Once()

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID:    orgID,
		ContinuationState: nil,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	ma.AssertExpectations(t)
}

func TestCustomDomainReconcileWorkflow_ListActivityFails(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
		Return(nil, errors.New("database connection failed")).Once()

	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID:    "",
		ContinuationState: nil,
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to list active domains")
}

func TestCustomDomainReconcileWorkflow_NilProcessedDomains(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	domains := []string{"a.com", "b.com"}

	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
		Return(domains, nil).Once()

	ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: "a.com"}).Return(nil).Once()
	ma.On("EnsureCustomDomainIngress", activities.EnsureCustomDomainIngressArgs{Domain: "b.com"}).Return(nil).Once()

	// ContinuationState is non-nil but ProcessedDomains is nil
	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID: "",
		ContinuationState: &ReconcileContinuationState{
			ProcessedDomains: nil, // nil slice
			Reconciled:       0,
			Failed:           0,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	ma.AssertExpectations(t)
}

func TestCustomDomainReconcileWorkflow_AllAlreadyProcessed(t *testing.T) {
	t.Parallel()

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	ma := &mockActivities{}
	env.RegisterActivity(ma.ListActiveCustomDomains)
	env.RegisterActivity(ma.EnsureCustomDomainIngress)

	domains := []string{"a.com", "b.com", "c.com"}

	ma.On("ListActiveCustomDomains", activities.ListActiveCustomDomainsArgs{OrganizationID: ""}).
		Return(domains, nil).Once()

	// All domains already processed - EnsureCustomDomainIngress should NOT be called
	env.ExecuteWorkflow(CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
		OrganizationID: "",
		ContinuationState: &ReconcileContinuationState{
			ProcessedDomains: []string{"a.com", "b.com", "c.com"},
			Reconciled:       3,
			Failed:           0,
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify EnsureCustomDomainIngress was never called
	ma.AssertNotCalled(t, "EnsureCustomDomainIngress", mock.Anything)
	ma.AssertExpectations(t)
}
