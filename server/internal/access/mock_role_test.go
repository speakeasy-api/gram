package access

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	mockRoleTimestamp       = "2024-12-28T07:55:09Z"
	mockMembershipTimestamp = "2024-11-15T15:04:05Z"
)

type MockRoleProvider struct {
	mock.Mock
}

func newMockRoleProvider(t *testing.T) *MockRoleProvider {
	t.Helper()

	roles := &MockRoleProvider{}
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			return roles.AssertExpectations(mockExpectationProbe{})
		}, 2*time.Second, 10*time.Millisecond)
	})

	return roles
}

type mockExpectationProbe struct{}

func (mockExpectationProbe) Logf(string, ...any) {}

func (mockExpectationProbe) Errorf(string, ...any) {}

func (mockExpectationProbe) FailNow() {}

func (m *MockRoleProvider) CreateRole(ctx context.Context, orgID string, opts thirdpartyworkos.CreateRoleOpts) (*thirdpartyworkos.Role, error) {
	args := m.Called(ctx, orgID, opts)
	if role, ok := args.Get(0).(*thirdpartyworkos.Role); ok {
		return role, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func (m *MockRoleProvider) UpdateRole(ctx context.Context, orgID string, roleSlug string, opts thirdpartyworkos.UpdateRoleOpts) (*thirdpartyworkos.Role, error) {
	args := m.Called(ctx, orgID, roleSlug, opts)
	if role, ok := args.Get(0).(*thirdpartyworkos.Role); ok {
		return role, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func (m *MockRoleProvider) DeleteRole(ctx context.Context, orgID string, roleSlug string) error {
	args := m.Called(ctx, orgID, roleSlug)
	return mockErr(args, 0)
}

func (m *MockRoleProvider) UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*thirdpartyworkos.Member, error) {
	args := m.Called(ctx, membershipID, roleSlug)
	if member, ok := args.Get(0).(*thirdpartyworkos.Member); ok {
		return member, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func (m *MockRoleProvider) GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*thirdpartyworkos.Member, error) {
	args := m.Called(ctx, workOSUserID, workOSOrgID)
	if member, ok := args.Get(0).(*thirdpartyworkos.Member); ok {
		return member, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func mockErr(args mock.Arguments, index int) error {
	err := args.Error(index)
	if err == nil {
		return nil
	}

	return fmt.Errorf("mock return error: %w", err)
}

func mockRole(id, name, slug, description string) thirdpartyworkos.Role {
	return thirdpartyworkos.Role{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Description: description,
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}
}

func mockSystemRole(id, name, slug string) thirdpartyworkos.Role {
	return mockRole(id, name, slug, "")
}

func mockMember(orgID, membershipID, userID, roleSlug string) thirdpartyworkos.Member {
	return thirdpartyworkos.Member{
		ID:             membershipID,
		UserID:         userID,
		OrganizationID: orgID,
		RoleSlug:       roleSlug,
		CreatedAt:      mockMembershipTimestamp,
	}
}
