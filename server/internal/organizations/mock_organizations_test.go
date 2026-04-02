package organizations_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type MockOrganizationProvider struct {
	mock.Mock
}

func newMockOrganizationProvider(t *testing.T) *MockOrganizationProvider {
	t.Helper()

	orgs := &MockOrganizationProvider{}
	t.Cleanup(func() {
		require.True(t, orgs.AssertExpectations(t))
	})

	return orgs
}

func (m *MockOrganizationProvider) SendInvitation(ctx context.Context, opts thirdpartyworkos.SendInvitationOpts) (*thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, opts)
	if invite, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invite, mockErr(args, 1)
	}
	return nil, mockErr(args, 0)
}

func (m *MockOrganizationProvider) ListInvitations(ctx context.Context, orgID string) ([]thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, orgID)
	if invitations, ok := args.Get(0).([]thirdpartyworkos.Invitation); ok {
		return invitations, mockErr(args, 1)
	}
	return nil, mockErr(args, 0)
}

func (m *MockOrganizationProvider) RevokeInvitation(ctx context.Context, invitationID string) (*thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, invitationID)
	if invitation, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invitation, mockErr(args, 1)
	}
	return nil, mockErr(args, 0)
}

func (m *MockOrganizationProvider) FindInvitationByToken(ctx context.Context, token string) (*thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, token)
	if invitation, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invitation, mockErr(args, 1)
	}
	return nil, mockErr(args, 0)
}

func (m *MockOrganizationProvider) GetInvitation(ctx context.Context, invitationID string) (*thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, invitationID)
	if invitation, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invitation, mockErr(args, 1)
	}
	return nil, mockErr(args, 0)
}

func (m *MockOrganizationProvider) ListUsers(ctx context.Context, orgID string) ([]thirdpartyworkos.User, error) {
	args := m.Called(ctx, orgID)
	if users, ok := args.Get(0).([]thirdpartyworkos.User); ok {
		return users, mockErr(args, 1)
	}
	return nil, mockErr(args, 0)
}

func (m *MockOrganizationProvider) RemoveUser(ctx context.Context, orgID, userID string) error {
	args := m.Called(ctx, orgID, userID)
	if err := args.Error(0); err != nil {
		return err
	}
	return nil
}

func mockErr(args mock.Arguments, index int) error {
	err := args.Error(index)
	if err == nil {
		return nil
	}

	return fmt.Errorf("mock return error: %w", err)
}
