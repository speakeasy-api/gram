package lifecycle

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// ReleaseActor is who an issuer release is audited under.
type ReleaseActor struct {
	// Principal is the user, agent or system the deletion is recorded for.
	Principal urn.Principal

	// DisplayName is the principal's display name, when it has one.
	DisplayName *string
}

// ReleaseUnownedIssuer deletes the project user session issuer issuerID once
// the caller's deletion leaves no live MCP server or gateway using it: its
// client bindings, sessions and consents go with it, and any grant whose client
// it was the last binding of is tombstoned. It returns those grants'
// credentials for [remotesessions.UpstreamRevoker.RevokeAllDetached] once the
// transaction commits. An issuer that is missing, organization-level or still
// owned is left alone. Call it after the owner is soft-deleted.
func ReleaseUnownedIssuer(ctx context.Context, tx pgx.Tx, revoker *remotesessions.UpstreamRevoker, auditLogger *audit.Logger, actor ReleaseActor, organizationID string, projectID, issuerID uuid.UUID) ([]remotesessions.RevokedCredentials, error) {
	q := repo.New(tx)
	if err := q.LockUserSessionIssuerForOwnerBinding(ctx, issuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock user session issuer for owner binding")
	}
	// Lock the issuer row before the ownership check. A concurrent gateway
	// attach holds this same row lock while writing its reference, so the
	// statements below see any newly committed owner. A missing issuer must not
	// block the deletion, so ErrNoRows skips the cascade entirely.
	_, lockErr := q.LockUserSessionIssuer(ctx, repo.LockUserSessionIssuerParams{ID: issuerID, ProjectID: projectID})
	if errors.Is(lockErr, pgx.ErrNoRows) {
		return nil, nil
	}
	if lockErr != nil {
		return nil, oops.E(oops.CodeUnexpected, lockErr, "lock user session issuer")
	}
	owned, err := q.UserSessionIssuerHasActiveOwner(ctx, repo.UserSessionIssuerHasActiveOwnerParams{
		ProjectID:           projectID,
		UserSessionIssuerID: issuerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check user session issuer ownership")
	}
	if owned {
		return nil, nil
	}
	if err := GuardEMABindings(ctx, tx, organizationID, projectID, issuerID); err != nil {
		return nil, err
	}
	deleted, err := q.DeleteUserSessionIssuer(ctx, repo.DeleteUserSessionIssuerParams{ID: issuerID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete user session issuer")
	}
	creds, err := revoker.DetachUserSessionIssuerFromClients(ctx, tx, deleted.ID, projectID, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "detach remote session clients from user session issuer")
	}
	if _, err := q.SoftDeleteUserSessionsByIssuerID(ctx, deleted.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete user session issuer sessions")
	}
	if _, err := q.SoftDeleteUserSessionConsentsByIssuerID(ctx, deleted.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete user session issuer consents")
	}
	if err := auditLogger.LogUserSessionIssuerDelete(ctx, tx, audit.LogUserSessionIssuerDeleteEvent{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		Actor:                actor.Principal,
		ActorDisplayName:     actor.DisplayName,
		ActorSlug:            nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(deleted.ID),
		Slug:                 deleted.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer deletion")
	}
	return creds, nil
}
