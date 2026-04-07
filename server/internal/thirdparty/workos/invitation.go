package workos

import (
	"context"
	"fmt"
	"net/http"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"github.com/workos/workos-go/v6/pkg/workos_errors"
)

type InvitationState string

const (
	InvitationStatePending  InvitationState = "pending"
	InvitationStateAccepted InvitationState = "accepted"
	InvitationStateExpired  InvitationState = "expired"
	InvitationStateRevoked  InvitationState = "revoked"
)

// Invitation represents a WorkOS invitation with the fields used by Gram.
type Invitation struct {
	ID                  string
	Email               string
	State               InvitationState
	AcceptedAt          string
	RevokedAt           string
	Token               string
	AcceptInvitationURL string
	OrganizationID      string
	InviterUserID       string
	ExpiresAt           string
	CreatedAt           string
	UpdatedAt           string
}

type SendInvitationOpts struct {
	Email          string `json:"email"`
	OrganizationID string `json:"organization_id,omitempty"`
	ExpiresInDays  int    `json:"expires_in_days,omitempty"`
	InviterUserID  string `json:"inviter_user_id,omitempty"`
	RoleSlug       string `json:"role_slug,omitempty"`
}

// SendInvitation creates an invitation for a user to join an organization.
func (wc *Client) SendInvitation(ctx context.Context, opts SendInvitationOpts) (*Invitation, error) {
	inv, err := wc.um.SendInvitation(ctx, usermanagement.SendInvitationOpts{
		Email:          opts.Email,
		OrganizationID: opts.OrganizationID,
		ExpiresInDays:  opts.ExpiresInDays,
		InviterUserID:  opts.InviterUserID,
		RoleSlug:       opts.RoleSlug,
	})
	if err != nil {
		return nil, fmt.Errorf("send invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// ListInvitations returns all invitations for an organization.
func (wc *Client) ListInvitations(ctx context.Context, orgID string) ([]Invitation, error) {
	var all []Invitation
	after := ""

	for {
		resp, err := wc.um.ListInvitations(ctx, usermanagement.ListInvitationsOpts{
			OrganizationID: orgID,
			Limit:          100,
			After:          after,
			Email:          "",
			Order:          "",
			Before:         "",
		})
		if err != nil {
			return nil, fmt.Errorf("list invitations: %w", err)
		}

		for _, inv := range resp.Data {
			all = append(all, convertInvitation(inv))
		}

		if resp.ListMetadata.After == "" {
			break
		}
		after = resp.ListMetadata.After
	}

	return all, nil
}

// RevokeInvitation revokes an invitation by ID.
func (wc *Client) RevokeInvitation(ctx context.Context, invitationID string) (*Invitation, error) {
	inv, err := wc.um.RevokeInvitation(ctx, usermanagement.RevokeInvitationOpts{Invitation: invitationID})
	if err != nil {
		return nil, fmt.Errorf("revoke invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// ResendInvitation resends an invitation by ID.
func (wc *Client) ResendInvitation(ctx context.Context, invitationID string) (*Invitation, error) {
	inv, err := wc.um.ResendInvitation(ctx, usermanagement.ResendInvitationOpts{Invitation: invitationID})
	if err != nil {
		return nil, fmt.Errorf("resend invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// FindInvitationByToken resolves an invitation from its token.
// Returns (nil, nil) when the token does not match any invitation (WorkOS 404).
func (wc *Client) FindInvitationByToken(ctx context.Context, token string) (*Invitation, error) {
	inv, err := wc.um.FindInvitationByToken(ctx, usermanagement.FindInvitationByTokenOpts{InvitationToken: token})
	if err != nil {
		var httpErr workos_errors.HTTPError
		if errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("find invitation by token: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}

// GetInvitation returns an invitation by ID.
// Returns (nil, nil) when the invitation does not exist (WorkOS 404).
func (wc *Client) GetInvitation(ctx context.Context, invitationID string) (*Invitation, error) {
	inv, err := wc.um.GetInvitation(ctx, usermanagement.GetInvitationOpts{Invitation: invitationID})
	if err != nil {
		var httpErr workos_errors.HTTPError
		if errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get invitation: %w", err)
	}

	converted := convertInvitation(inv)
	return &converted, nil
}
