package organizations

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// PendingInvitation is a live invitation addressed to a signed-in user.
type PendingInvitation struct {
	ID               string
	OrganizationID   string
	OrganizationName string
}

// PendingInvitationsForEmail lists the live invitations addressed to email,
// newest first. Invitations to a disabled organization are left out, because
// they cannot be joined. Pass only an email the identity provider verified:
// holding it is what entitles a user to the invitation.
func (s *Service) PendingInvitationsForEmail(ctx context.Context, email string) ([]PendingInvitation, error) {
	rows, err := orgrepo.New(s.db).ListPendingInvitationsForEmail(ctx, conv.NormalizeEmail(email))
	if err != nil {
		return nil, fmt.Errorf("list pending invitations: %w", err)
	}
	invitations := make([]PendingInvitation, 0, len(rows))
	for _, row := range rows {
		invitations = append(invitations, PendingInvitation{
			ID:               row.ID.String(),
			OrganizationID:   row.OrganizationID,
			OrganizationName: row.OrganizationName,
		})
	}
	return invitations, nil
}

// AcceptPendingInvitationForUser accepts an invitation for a user who is
// already signed in, as the invite link does for one who follows it. The
// identity provider must have verified verifiedEmail for this sign-in, it
// must be the user's account email, and the invitation must be one
// PendingInvitationsForEmail lists for it.
func (s *Service) AcceptPendingInvitationForUser(ctx context.Context, userID, verifiedEmail, invitationID string) (PendingInvitation, error) {
	var empty PendingInvitation

	email := conv.NormalizeEmail(verifiedEmail)
	if email == "" {
		return empty, errors.New("invitation requires a verified email")
	}
	user, err := userrepo.New(s.db).GetUser(ctx, userID)
	if err != nil {
		return empty, fmt.Errorf("read user for invitation: %w", err)
	}
	if conv.NormalizeEmail(user.Email) != email {
		return empty, errors.New("verified email does not match the user")
	}
	if !user.WorkosID.Valid || user.WorkosID.String == "" {
		return empty, errors.New("user has no WorkOS identity")
	}

	pending, err := s.PendingInvitationsForEmail(ctx, email)
	if err != nil {
		return empty, err
	}
	i := slices.IndexFunc(pending, func(invitation PendingInvitation) bool { return invitation.ID == invitationID })
	if i < 0 {
		return empty, errors.New("invitation is not pending for this user")
	}
	invitation := pending[i]

	if err := s.acceptInvitation(ctx, uuid.MustParse(invitation.ID), invitation.OrganizationID, userID, user.WorkosID.String, email); err != nil {
		return empty, fmt.Errorf("accept invitation: %w", err)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, userID); err != nil {
		s.logger.WarnContext(ctx, "failed to invalidate user info cache after accepting invitation", attr.SlogError(err), attr.SlogUserID(userID))
	}
	return invitation, nil
}
