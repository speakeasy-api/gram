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
	if err := args.Error(1); err != nil {
		inv, _ := args.Get(0).(*thirdpartyworkos.Invitation)
		return inv, mockErr(args, 1)
	}
	if invite, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invite, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) ListInvitations(ctx context.Context, orgID string) ([]thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, orgID)
	if err := args.Error(1); err != nil {
		var list []thirdpartyworkos.Invitation
		if v, ok := args.Get(0).([]thirdpartyworkos.Invitation); ok {
			list = v
		}
		return list, mockErr(args, 1)
	}
	if invitations, ok := args.Get(0).([]thirdpartyworkos.Invitation); ok {
		return invitations, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) RevokeInvitation(ctx context.Context, invitationID string) (*thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, invitationID)
	if err := args.Error(1); err != nil {
		inv, _ := args.Get(0).(*thirdpartyworkos.Invitation)
		return inv, mockErr(args, 1)
	}
	if invitation, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invitation, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) FindInvitationByToken(ctx context.Context, token string) (*thirdpartyworkos.Invitation, error) {
	args := m.Called(ctx, token)
	if err := args.Error(1); err != nil {
		inv, _ := args.Get(0).(*thirdpartyworkos.Invitation)
		return inv, mockErr(args, 1)
	}
	if invitation, ok := args.Get(0).(*thirdpartyworkos.Invitation); ok {
		return invitation, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) DeleteOrganizationMembership(ctx context.Context, workosMembershipID string) error {
	args := m.Called(ctx, workosMembershipID)
	if err := args.Error(0); err != nil {
		return err
	}
	return nil
}

func (m *MockOrganizationProvider) GetUserByEmail(ctx context.Context, email string) (*thirdpartyworkos.User, error) {
	args := m.Called(ctx, email)
	if err := args.Error(1); err != nil {
		u, _ := args.Get(0).(*thirdpartyworkos.User)
		return u, mockErr(args, 1)
	}
	if u, ok := args.Get(0).(*thirdpartyworkos.User); ok {
		return u, nil
	}
	return nil, nil
}

func mockErr(args mock.Arguments, index int) error {
	err := args.Error(index)
	if err == nil {
		return nil
	}

	return fmt.Errorf("mock return error: %w", err)
}
