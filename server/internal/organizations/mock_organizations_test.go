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

func (m *MockOrganizationProvider) CreatePasswordlessSession(ctx context.Context, opts thirdpartyworkos.CreatePasswordlessSessionOpts) (*thirdpartyworkos.PasswordlessSession, error) {
	args := m.Called(ctx, opts)
	if err := args.Error(1); err != nil {
		sess, _ := args.Get(0).(*thirdpartyworkos.PasswordlessSession)
		return sess, mockErr(args, 1)
	}
	if sess, ok := args.Get(0).(*thirdpartyworkos.PasswordlessSession); ok {
		return sess, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) AuthenticateWithInviteLink(ctx context.Context, code string) (*thirdpartyworkos.InviteLinkProfile, error) {
	args := m.Called(ctx, code)
	if err := args.Error(1); err != nil {
		return nil, mockErr(args, 1)
	}
	if profile, ok := args.Get(0).(*thirdpartyworkos.InviteLinkProfile); ok {
		return profile, nil
	}
	return nil, nil
}

func (m *MockOrganizationProvider) DeleteOrganizationMembership(ctx context.Context, workosMembershipID string) error {
	args := m.Called(ctx, workosMembershipID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock DeleteOrganizationMembership: %w", err)
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
