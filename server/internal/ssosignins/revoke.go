package ssosignins

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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
// sign-ins or grants, inside the caller's transaction. Per-grant entries are
// written after commit by AuditRevokedGrants.
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
	return nil
}

// auditBatchSize bounds the per-grant audit entries written per transaction.
const auditBatchSize = 100

// AuditRevokedGrants writes one entry per soft-deleted grant after the
// revocation committed, in bounded transactions so no lock is held across
// them. Failures are logged; the summary entry already records the counts.
func AuditRevokedGrants(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger, actor Actor, sessions []RevokedSession) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postCommitTimeout)
	defer cancel()
	for batch := range slices.Chunk(sessions, auditBatchSize) {
		if err := auditGrantBatch(ctx, db, auditLogger, actor, batch); err != nil {
			logger.ErrorContext(ctx, "audit revoked grants", attr.SlogValueInt(len(batch)), attr.SlogError(err))
		}
	}
}

func auditGrantBatch(ctx context.Context, db *pgxpool.Pool, auditLogger *audit.Logger, actor Actor, batch []RevokedSession) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	for _, session := range batch {
		if err := auditLogger.LogUserSessionRevoke(ctx, tx, audit.LogUserSessionRevokeEvent{
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
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// postCommitTimeout bounds post-commit work, detached from the caller's
// cancellation so a dropped request cannot strand it.
const postCommitTimeout = 15 * time.Second
