package organizations_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type MockOrganizationProvider struct {
	mock.Mock
}

var _ organizations.OrganizationProvider = (*MockOrganizationProvider)(nil)

func newMockOrganizationProvider(t *testing.T) *MockOrganizationProvider {
	t.Helper()

	orgs := &MockOrganizationProvider{}
	t.Cleanup(func() {
		require.True(t, orgs.AssertExpectations(t))
	})

	return orgs
}

func (m *MockOrganizationProvider) CreateOrganizationMembership(ctx context.Context, workosUserID, workosOrgID, roleSlug string) (string, error) {
	args := m.Called(ctx, workosUserID, workosOrgID, roleSlug)
	if err := args.Error(1); err != nil {
		return "", fmt.Errorf("mock CreateOrganizationMembership: %w", err)
	}
	return args.String(0), nil
}

func (m *MockOrganizationProvider) ListRoles(ctx context.Context, workosOrgID string) ([]thirdpartyworkos.Role, error) {
	args := m.Called(ctx, workosOrgID)
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock ListRoles: %w", err)
	}
	if roles, ok := args.Get(0).([]thirdpartyworkos.Role); ok {
		return roles, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) GetOrganizationDomainPolicy(ctx context.Context, workosOrgID string) (*thirdpartyworkos.OrganizationDomainPolicy, error) {
	for _, call := range m.ExpectedCalls {
		if call.Method == "GetOrganizationDomainPolicy" {
			args := m.Called(ctx, workosOrgID)
			if err := args.Error(1); err != nil {
				return nil, fmt.Errorf("mock GetOrganizationDomainPolicy: %w", err)
			}
			if policy, ok := args.Get(0).(*thirdpartyworkos.OrganizationDomainPolicy); ok {
				return policy, nil
			}
			return nil, nil
		}
	}

	return &thirdpartyworkos.OrganizationDomainPolicy{
		Domains: nil,
	}, nil
}

func (m *MockOrganizationProvider) DeleteOrganizationMembership(ctx context.Context, workosMembershipID string) error {
	args := m.Called(ctx, workosMembershipID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock DeleteOrganizationMembership: %w", err)
	}
	return nil
}

// stubUserProvisioner is a no-op implementation of UserProvisioner for tests
// that don't exercise the invite callback HTTP handler.
type stubUserProvisioner struct{}

func (stubUserProvisioner) AuthenticateWithMagicAuth(_ context.Context, email string) (*identity.IDPUserInfo, error) {
	return &identity.IDPUserInfo{
		Sub:             "user_01INVITEE",
		Email:           email,
		Name:            "Invitee",
		Picture:         nil,
		ExternalID:      "",
		WorkOSSessionID: "session_01INVITE",
		OrganizationID:  "",
	}, nil
}

func (stubUserProvisioner) UpsertUserFromIDP(_ context.Context, idpUser *identity.IDPUserInfo) (string, error) {
	return idpUser.Sub, nil
}

// MockLoopsClient implements loops.Client for testing email send paths.
type MockLoopsClient struct {
	mock.Mock
}

var _ loops.Client = (*MockLoopsClient)(nil)

func newMockLoopsClient(t *testing.T) *MockLoopsClient {
	t.Helper()
	c := &MockLoopsClient{}
	t.Cleanup(func() {
		require.True(t, c.AssertExpectations(t))
	})
	return c
}

func (m *MockLoopsClient) SendTransactional(ctx context.Context, input loops.SendTransactionalInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}
