package organizations

import (
	"context"

	"github.com/google/uuid"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// AcceptInvitationForTest exposes acceptInvitation so a test can check the
// policy run under the organization lock.
func (s *Service) AcceptInvitationForTest(ctx context.Context, inviteID uuid.UUID, organizationID, gramUserID, workosUserID, inviteeEmail string, requireSSO func(orgrepo.OrganizationMetadatum) bool) error {
	return s.acceptInvitation(ctx, inviteID, organizationID, gramUserID, workosUserID, inviteeEmail, requireSSO)
}
