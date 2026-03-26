package workos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

type WorkOS struct {
	logger *slog.Logger
	um     *usermanagement.Client
}

func New(logger *slog.Logger, apiKey string) *WorkOS {
	if apiKey == "" || apiKey == "unset" {
		return nil
	}

	return &WorkOS{
		logger: logger,
		um:     usermanagement.NewClient(apiKey),
	}
}

// NewForTest creates a WorkOS client backed by a custom usermanagement.Client.
// This is intended for use in tests where the endpoint and HTTP client are overridden.
func NewForTest(logger *slog.Logger, client *usermanagement.Client) *WorkOS {
	return &WorkOS{
		logger: logger,
		um:     client,
	}
}

func (w *WorkOS) GetUserByEmail(ctx context.Context, email string) (*usermanagement.User, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	user, err := w.um.ListUsers(ctx, usermanagement.ListUsersOpts{
		Email:          email,
		Limit:          1,
		OrganizationID: "",
		Order:          "",
		Before:         "",
		After:          "",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list users from workos: %w", err)
	}

	if len(user.Data) == 0 {
		return nil, nil
	}

	return &user.Data[0], nil
}

func (w *WorkOS) ListUsersInOrg(ctx context.Context, workOSOrgID string) ([]usermanagement.User, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	var allUsers []usermanagement.User
	after := ""

	for {
		resp, err := w.um.ListUsers(ctx, usermanagement.ListUsersOpts{
			OrganizationID: workOSOrgID,
			Limit:          100,
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list users from workos: %w", err)
		}

		allUsers = append(allUsers, resp.Data...)

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return allUsers, nil
}

func (w *WorkOS) SendInvitation(ctx context.Context, opts usermanagement.SendInvitationOpts) (usermanagement.Invitation, error) {
	if w == nil {
		return usermanagement.Invitation{}, errors.New("workos client is not initialized")
	}

	inv, err := w.um.SendInvitation(ctx, opts)
	if err != nil {
		return usermanagement.Invitation{}, fmt.Errorf("failed to send invitation via workos: %w", err)
	}

	return inv, nil
}

func (w *WorkOS) ListInvitations(ctx context.Context, workOSOrgID string) ([]usermanagement.Invitation, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	var allInvites []usermanagement.Invitation
	after := ""

	for {
		resp, err := w.um.ListInvitations(ctx, usermanagement.ListInvitationsOpts{
			OrganizationID: workOSOrgID,
			Limit:          100,
			After:          after,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list invitations from workos: %w", err)
		}

		allInvites = append(allInvites, resp.Data...)

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return allInvites, nil
}

func (w *WorkOS) RevokeInvitation(ctx context.Context, invitationID string) (usermanagement.Invitation, error) {
	if w == nil {
		return usermanagement.Invitation{}, errors.New("workos client is not initialized")
	}

	inv, err := w.um.RevokeInvitation(ctx, usermanagement.RevokeInvitationOpts{
		Invitation: invitationID,
	})
	if err != nil {
		return usermanagement.Invitation{}, fmt.Errorf("failed to revoke invitation via workos: %w", err)
	}

	return inv, nil
}

func (w *WorkOS) ResendInvitation(ctx context.Context, invitationID string) (usermanagement.Invitation, error) {
	if w == nil {
		return usermanagement.Invitation{}, errors.New("workos client is not initialized")
	}

	inv, err := w.um.ResendInvitation(ctx, usermanagement.ResendInvitationOpts{
		Invitation: invitationID,
	})
	if err != nil {
		return usermanagement.Invitation{}, fmt.Errorf("failed to resend invitation via workos: %w", err)
	}

	return inv, nil
}

func (w *WorkOS) FindInvitationByToken(ctx context.Context, token string) (usermanagement.Invitation, error) {
	if w == nil {
		return usermanagement.Invitation{}, errors.New("workos client is not initialized")
	}

	inv, err := w.um.FindInvitationByToken(ctx, usermanagement.FindInvitationByTokenOpts{
		InvitationToken: token,
	})
	if err != nil {
		return usermanagement.Invitation{}, fmt.Errorf("failed to find invitation by token via workos: %w", err)
	}

	return inv, nil
}

func (w *WorkOS) GetUser(ctx context.Context, userID string) (*usermanagement.User, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	user, err := w.um.GetUser(ctx, usermanagement.GetUserOpts{
		User: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user from workos: %w", err)
	}

	return &user, nil
}

func (w *WorkOS) DeleteOrganizationMembership(ctx context.Context, membershipID string) error {
	if w == nil {
		return errors.New("workos client is not initialized")
	}

	err := w.um.DeleteOrganizationMembership(ctx, usermanagement.DeleteOrganizationMembershipOpts{
		OrganizationMembership: membershipID,
	})
	if err != nil {
		return fmt.Errorf("failed to delete organization membership via workos: %w", err)
	}

	return nil
}

func (w *WorkOS) GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*usermanagement.OrganizationMembership, error) {
	if w == nil {
		return nil, errors.New("workos client is not initialized")
	}

	membership, err := w.um.ListOrganizationMemberships(
		ctx,
		usermanagement.ListOrganizationMembershipsOpts{
			UserID:         workOSUserID,
			OrganizationID: workOSOrgID,
			Limit:          1,
			Statuses:       nil,
			Order:          "",
			Before:         "",
			After:          "",
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to list organization memberships from workos: %w", err)
	}

	if len(membership.Data) == 0 {
		return nil, nil
	}

	return &membership.Data[0], nil
}
