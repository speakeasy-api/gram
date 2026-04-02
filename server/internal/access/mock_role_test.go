package access_test

import (
	"context"
	"fmt"
	"testing"

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
		require.True(t, roles.AssertExpectations(t))
	})

	return roles
}

func (m *MockRoleProvider) ListRoles(ctx context.Context, orgID string) ([]thirdpartyworkos.Role, error) {
	args := m.Called(ctx, orgID)
	if roles, ok := args.Get(0).([]thirdpartyworkos.Role); ok {
		return roles, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

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

func (m *MockRoleProvider) ListMembers(ctx context.Context, orgID string) ([]thirdpartyworkos.Member, error) {
	args := m.Called(ctx, orgID)
	if members, ok := args.Get(0).([]thirdpartyworkos.Member); ok {
		return members, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func (m *MockRoleProvider) UpdateMemberRole(ctx context.Context, membershipID string, roleSlug string) (*thirdpartyworkos.Member, error) {
	args := m.Called(ctx, membershipID, roleSlug)
	if member, ok := args.Get(0).(*thirdpartyworkos.Member); ok {
		return member, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func (m *MockRoleProvider) GetUser(ctx context.Context, userID string) (*thirdpartyworkos.User, error) {
	args := m.Called(ctx, userID)
	if user, ok := args.Get(0).(*thirdpartyworkos.User); ok {
		return user, mockErr(args, 1)
	}
	return nil, mockErr(args, 1)
}

func (m *MockRoleProvider) ListOrgUsers(ctx context.Context, orgID string) (map[string]thirdpartyworkos.User, error) {
	args := m.Called(ctx, orgID)
	if users, ok := args.Get(0).(map[string]thirdpartyworkos.User); ok {
		return users, mockErr(args, 1)
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

func mockUser(id, firstName, lastName, email string) thirdpartyworkos.User {
	return thirdpartyworkos.User{
		ID:        id,
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
	}
}
