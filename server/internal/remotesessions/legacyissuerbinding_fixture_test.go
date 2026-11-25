package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LegacyIssuerBindingForTest is input for the test-only compatibility writer.
// Some regressions need to exercise synchronization from a legacy join write;
// these helpers are not part of the service's production API.
type LegacyIssuerBindingForTest struct {
	ID                  string
	UserSessionIssuerID string
}

// AttachLegacyIssuerForTest records a remote_session_client / user_session_issuer
// binding in the join table. The pairing is rejected when another client is
// already bound to the same user_session_issuer for this client's
// remote_session_issuer.
func (s *Service) AttachLegacyIssuerForTest(ctx context.Context, payload *LegacyIssuerBindingForTest) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	userIssuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx) //nolint:glint // notestingrawsql: the fixture uses generated queries and needs one transaction for the legacy write and typed sync.
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Capture and typed synchronization share this transaction. Acquire the
	// complete writer scope before issuer/client locks or binding mutations.
	scope, err := lockBindingWriteScope(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, clientID, []uuid.UUID{userIssuerID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock client binding writer scope").LogError(ctx, logger)
	}

	txRepo := repo.New(dbtx)

	// Resolve the client (the project's own or an organization-level client in
	// the project's org) so a project admin can attach an org-level client to
	// their own user_session_issuer.
	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	if err := lockUserSessionIssuersForClientBinding(ctx, logger, dbtx, txRepo, *authCtx.ProjectID, authCtx.ActiveOrganizationID, []uuid.UUID{userIssuerID}); err != nil {
		return nil, err
	}
	if err := s.refuseLegacyOrgWideAttachmentForTest(ctx, logger, txRepo, *authCtx, userIssuerID, clientID, true); err != nil {
		return nil, err
	}

	// Exclude this client so re-attaching an existing binding is a no-op.
	if err := s.guardSingleClientPerRemoteIssuer(ctx, logger, txRepo, authCtx.ActiveOrganizationID, *authCtx.ProjectID, userIssuerID, existing.RemoteSessionClient.RemoteSessionIssuerID, clientID); err != nil {
		return nil, err
	}

	if err := txRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach remote session client to user session issuer").LogError(ctx, logger)
	}

	return s.commitLegacyClientAttachmentChangeForTest(ctx, logger, dbtx, txRepo, *authCtx, scope, clientID, []uuid.UUID{userIssuerID}, func(ctx context.Context, dbtx pgx.Tx) error {
		return s.auditLogger.LogRemoteSessionClientAttachUserSessionIssuer(ctx, dbtx, audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(clientID),
			ClientID:               existing.RemoteSessionClient.ClientID,
			UserSessionIssuerURN:   urn.NewUserSessionIssuer(userIssuerID),
		})
	})
}

// DetachLegacyIssuerForTest removes a remote_session_client / user_session_issuer
// binding from the join table. Detaching a binding that does not exist is a
// no-op.
func (s *Service) DetachLegacyIssuerForTest(ctx context.Context, payload *LegacyIssuerBindingForTest) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	userIssuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx) //nolint:glint // notestingrawsql: the fixture uses generated queries and needs one transaction for the legacy write and typed sync.
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// Capture and typed synchronization share this transaction. Acquire the
	// complete writer scope before issuer/client locks or binding mutations.
	scope, err := lockBindingWriteScope(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, clientID, []uuid.UUID{userIssuerID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock client binding writer scope").LogError(ctx, logger)
	}

	txRepo := repo.New(dbtx)

	// Resolve the client (the project's own or an organization-level client in
	// the project's org) before mutating the project-agnostic join table, so a
	// project admin can detach an org-level client from their own
	// user_session_issuer.
	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	if err := s.refuseLegacyOrgWideAttachmentForTest(ctx, logger, txRepo, *authCtx, userIssuerID, clientID, false); err != nil {
		return nil, err
	}

	// Lock in preparation's user-issuer -> client order. The scoped lock
	// rechecks client ownership after waiting; the issuer remains locked, and
	// the mutation repeats both tenant predicates rather than trusting IDs.
	locked, err := txRepo.LockEMAClient(ctx, repo.LockEMAClientParams{ID: clientID, ProjectID: conv.ToNullUUID(*authCtx.ProjectID), OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID)})
	if err != nil {
		return nil, lifecycleLockError(err)
	}
	existing.RemoteSessionClient = locked
	attached, err := lockAttachmentParentForUnlink(ctx, txRepo, authCtx.ActiveOrganizationID, clientID, userIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check agent attachments before unlink").LogError(ctx, logger)
	}
	if attached {
		return nil, oops.E(oops.CodeConflict, nil, agentBindingsOnReplacedClientMessage).LogWarn(ctx, logger)
	}
	removed, err := txRepo.DetachProjectRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachProjectRemoteSessionClientFromUserSessionIssuerParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "detach remote session client from user session issuer").LogError(ctx, logger)
	}
	if removed > 0 {
		// A conflict rolls back the tentative removal with the transaction.
		projectID := *authCtx.ProjectID
		if !locked.ProjectID.Valid {
			// An organization client can have a shared organization issuer link.
			// Protect every project using that association, not only the caller.
			projectID = uuid.Nil
		}
		if err := guardEMABindingsForClientUserIssuer(ctx, txRepo, authCtx.ActiveOrganizationID, projectID, clientID, userIssuerID); err != nil {
			return nil, err
		}
	}

	return s.commitLegacyClientAttachmentChangeForTest(ctx, logger, dbtx, txRepo, *authCtx, scope, clientID, []uuid.UUID{userIssuerID}, func(ctx context.Context, dbtx pgx.Tx) error {
		return s.auditLogger.LogRemoteSessionClientDetachUserSessionIssuer(ctx, dbtx, audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(clientID),
			ClientID:               existing.RemoteSessionClient.ClientID,
			UserSessionIssuerURN:   urn.NewUserSessionIssuer(userIssuerID),
		})
	})
}

// refuseLegacyOrgWideAttachmentForTest refuses a project-scoped attach or detach of an
// organization-level client on an organization-level user session issuer by a
// caller without organization admin: that binding is shared by every project's
// servers on the issuer.
func (s *Service) refuseLegacyOrgWideAttachmentForTest(ctx context.Context, logger *slog.Logger, q *repo.Queries, authCtx contextvalues.AuthContext, userIssuerID, clientID uuid.UUID, attaching bool) error {
	// Scope and lock the issuer before checking the binding, even for a no-op.
	// Attach also holds the owner advisory lock; the row lock serializes both
	// paths against organization detach and preserves issuer-before-client order.
	if _, err := q.LockProjectUserIssuerForDetach(ctx, repo.LockProjectUserIssuerForDetachParams{
		ID: userIssuerID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}
	// Re-read after acquiring the lock: the pre-lock snapshot may describe a
	// no-op that is now an organization-wide change.
	client, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID: clientID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}
	if slices.Contains(client.UserSessionIssuerIds, userIssuerID) == attaching {
		return nil // Re-attaching or detaching a missing binding is a no-op.
	}
	issuer, err := q.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             userIssuerID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}
	orgAdmin, err := s.actorIsOrganizationAdmin(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check organization admin access").LogError(ctx, logger)
	}
	if err := refuseOrgWideBinding(!issuer.ProjectID.Valid, orgAdmin, client.RemoteSessionClient.ProjectID); err != nil {
		return identityOopsError(err, "attach remote session client").LogError(ctx, logger)
	}
	return nil
}

// commitLegacyClientAttachmentChangeForTest re-reads a client after an attach/detach, records
// the supplied attachment audit event and synchronizes derived state in the
// same transaction, commits, and
// returns the after view. auditFn lets each caller emit the right action
// (attach vs detach) while the re-read/commit stays shared.
func (s *Service) commitLegacyClientAttachmentChangeForTest(
	ctx context.Context,
	logger *slog.Logger,
	dbtx pgx.Tx,
	txRepo *repo.Queries,
	authCtx contextvalues.AuthContext,
	scope bindingWriteScope,
	clientID uuid.UUID,
	touchedUserIssuerIDs []uuid.UUID,
	auditFn func(ctx context.Context, dbtx pgx.Tx) error,
) (*types.RemoteSessionClient, error) {
	updated, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	afterView, err := s.clientView(updated.RemoteSessionClient, updated.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	if err := auditFn(ctx, dbtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client attachment change").LogError(ctx, logger)
	}

	// A successful binding write must include its captured associations and
	// typed state. On failure the caller rolls back the entire DB mutation.
	if err := scope.resync(ctx, dbtx, *authCtx.ProjectID, touchedUserIssuerIDs, clientID); err != nil {
		return nil, bindingSyncError(ctx, logger, err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}
