package signins

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TokenRevoker pushes access-token JTIs into the revocation cache.
type TokenRevoker interface {
	RevokeToken(ctx context.Context, jti string) error
}

// IDPSessionRevoker ends identity provider (WorkOS) sessions.
type IDPSessionRevoker interface {
	RevokeSession(ctx context.Context, sessionID string) error
}

// Actor is who a revocation is attributed to in the audit log.
type Actor struct {
	Principal   urn.Principal
	DisplayName *string
}

// AuditRevocation writes, inside the caller's transaction, one summary entry
// per organization that lost sign-ins or grants and one entry per
// soft-deleted grant, so the audit log commits with the revocation.
func AuditRevocation(ctx context.Context, dbtx auditrepo.DBTX, logger *audit.Logger, actor Actor, userID, userEmail string, reason RevokeReason, organizationIDs []string, revoked Revoked) error {
	for _, organizationID := range organizationIDs {
		signIns := revoked.SignInsByOrganization[organizationID]
		sessions := revoked.SessionsInOrganization(organizationID)
		if signIns == 0 && sessions == 0 {
			continue
		}
		if err := logger.LogAccessMemberSignInsRevoke(ctx, dbtx, audit.LogAccessMemberSignInsRevokeEvent{
			OrganizationID:   organizationID,
			Actor:            actor.Principal,
			ActorDisplayName: actor.DisplayName,
			MemberID:         userID,
			MemberEmail:      userEmail,
			Reason:           string(reason),
			RevokedSignIns:   signIns,
			RevokedSessions:  sessions,
		}); err != nil {
			return fmt.Errorf("log sign-in revocation: %w", err)
		}
	}
	for _, session := range revoked.Sessions {
		if err := logger.LogUserSessionRevoke(ctx, dbtx, audit.LogUserSessionRevokeEvent{
			OrganizationID:   session.OrganizationID,
			ProjectID:        session.ProjectID,
			Actor:            actor.Principal,
			ActorDisplayName: actor.DisplayName,
			ActorSlug:        nil,
			UserSessionURN:   urn.NewUserSession(session.ID),
			Principal:        session.Subject,
			Jti:              session.JTI,
		}); err != nil {
			return fmt.Errorf("log user session revocation: %w", err)
		}
	}
	return nil
}
