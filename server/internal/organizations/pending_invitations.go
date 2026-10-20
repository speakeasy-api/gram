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

// ErrInvitationRequiresSSO refuses an invitation to an organization whose
// SSO connection covers the invitee's email domain when the user did not sign
// in through that organization's SSO. WorkOS sends such an address to the
// organization's SSO at sign-in, so joining any other way would bypass it.
var ErrInvitationRequiresSSO = errors.New("invitation requires signing in with SSO")

// PendingInvitation is a live invitation addressed to a signed-in user.
type PendingInvitation struct {
	ID               string
	OrganizationID   string
	OrganizationName string
	// RequiresSSO is true when the organization has an active SSO connection
	// covering the invitee's email domain and the sign-in did not go through
	// it. Such an invitation cannot be joined from this sign-in.
	RequiresSSO bool
}

// PendingInvitationsForEmail lists the live invitations addressed to email,
// newest first. Invitations to a disabled organization are left out, because
// they cannot be joined. Pass only an email the identity provider verified:
// holding it is what entitles a user to the invitation. ssoWorkOSOrganizationID
// is the WorkOS organization whose SSO connection the user signed in through,
// or empty for any other sign-in method.
func (s *Service) PendingInvitationsForEmail(ctx context.Context, email, ssoWorkOSOrganizationID string) ([]PendingInvitation, error) {
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
			RequiresSSO:      invitationRequiresSSO(email, ssoWorkOSOrganizationID, row.SsoEnabled, row.VerifiedDomains, row.OrganizationWorkosID.String),
		})
	}
	return invitations, nil
}

// invitationRequiresSSO reports whether an organization's SSO covers email
// and the sign-in did not go through that organization's SSO.
func invitationRequiresSSO(email, ssoWorkOSOrganizationID string, ssoEnabled bool, verifiedDomains []string, orgWorkOSID string) bool {
	return ssoEnabled && emailOnVerifiedDomain(email, verifiedDomains) && (ssoWorkOSOrganizationID == "" || orgWorkOSID != ssoWorkOSOrganizationID)
}

func emailOnVerifiedDomain(email string, verified []string) bool {
	domain, ok := inviteEmailDomain(email)
	return ok && slices.ContainsFunc(verified, func(d string) bool { return normalizeInviteDomain(d) == domain })
}

// AcceptPendingInvitationForUser accepts an invitation for a user who is
// already signed in, as the invite link does for one who follows it. The
// identity provider must have verified verifiedEmail for this sign-in, it
// must be the user's account email, and the invitation must be one
// PendingInvitationsForEmail lists for it. An invitation that requires SSO is
// refused with ErrInvitationRequiresSSO.
func (s *Service) AcceptPendingInvitationForUser(ctx context.Context, userID, verifiedEmail, ssoWorkOSOrganizationID, invitationID string) (PendingInvitation, error) {
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

	pending, err := s.PendingInvitationsForEmail(ctx, email, ssoWorkOSOrganizationID)
	if err != nil {
		return empty, err
	}
	i := slices.IndexFunc(pending, func(invitation PendingInvitation) bool { return invitation.ID == invitationID })
	if i < 0 {
		return empty, errors.New("invitation is not pending for this user")
	}
	invitation := pending[i]
	if invitation.RequiresSSO {
		return empty, ErrInvitationRequiresSSO
	}

	// Checked again under the organization lock, so SSO enabled or a domain
	// verified after the listing above still applies.
	requireSSO := func(org orgrepo.OrganizationMetadatum) bool {
		return invitationRequiresSSO(email, ssoWorkOSOrganizationID, org.SsoEnabled.Bool, org.VerifiedDomains, org.WorkosID.String)
	}
	if err := s.acceptInvitation(ctx, uuid.MustParse(invitation.ID), invitation.OrganizationID, userID, user.WorkosID.String, email, requireSSO); err != nil {
		return empty, fmt.Errorf("accept invitation: %w", err)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, userID); err != nil {
		s.logger.WarnContext(ctx, "failed to invalidate user info cache after accepting invitation", attr.SlogError(err), attr.SlogUserID(userID))
	}
	return invitation, nil
}
