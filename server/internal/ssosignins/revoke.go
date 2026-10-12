package ssosignins

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
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

// AuditRevocation writes one summary entry per organization that lost
// sign-ins or grants, then one entry per soft-deleted grant, inside the
// caller's transaction.
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

// postCommitTimeout bounds the post-commit pushes, detached from the caller's
// cancellation so a dropped request cannot leave revoked tokens live.
const postCommitTimeout = 15 * time.Second

// FinishRevocation runs after commit: it pushes JTIs into the revocation
// cache and ends WorkOS sessions. Failures are logged with counts, never
// returned: the database already holds the revocation, and a retried call
// re-pushes recent JTIs.
func FinishRevocation(ctx context.Context, logger *slog.Logger, tokens TokenRevoker, idp IDPSessionRevoker, jtis []string, workosSessionIDs []string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postCommitTimeout)
	defer cancel()

	if tokens != nil {
		failed := 0
		var firstErr error
		for _, jti := range jtis {
			if err := tokens.RevokeToken(ctx, jti); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if failed > 0 {
			logger.ErrorContext(ctx, "push revoked jtis into revocation cache", attr.SlogValueInt(failed), attr.SlogError(firstErr))
		}
	}
	if idp != nil {
		failed := 0
		var firstErr error
		for _, id := range workosSessionIDs {
			if id == "" {
				continue
			}
			if err := idp.RevokeSession(ctx, id); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if failed > 0 {
			logger.ErrorContext(ctx, "revoke workos sessions", attr.SlogValueInt(failed), attr.SlogError(firstErr))
		}
	}
}
