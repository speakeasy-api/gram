package remotesessions

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// requireKeySetEntitlement gates the JSON Web Key Set link on the same
// entitlement as the sets themselves and the external keys beneath them
// (jsonwebkeysets, externalkeys, externalcredentials all apply it in their
// requireOrgAccess). Gating the link is not an inherited default: a set is
// always backed by a customer-provisioned KMS key, because
// json_web_key_sets.external_key_id is NOT NULL and chains to an external_keys
// row with provider IN ('aws_kms','gcp_kms'). There is no Gram-managed key
// path, so an organization without the entitlement has no set to attach and
// this refusal is the honest answer rather than an upsell.
//
// Deliberately scoped to attach and detach. The rest of remote_session_client
// management stays ungated, so an organization that never bought CMEK keeps
// creating, updating, and deleting clients exactly as before.
func (s *Service) requireKeySetEntitlement(ctx context.Context, logger *slog.Logger, organizationID string) error {
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureCustomerManagedEncryptionKeys)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error checking customer managed keys entitlement").LogError(ctx, logger)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "customer-managed encryption keys are not enabled for this organization")
	}

	return nil
}

// adoptClientOrganization makes a client eligible to hold a JSON Web Key Set,
// backfilling its organization_id from its project when the column was never
// populated.
//
// remote_session_clients.organization_id is nullable and was added without a
// backfill (20260625174452), so rows predating it are still NULL, and
// remote_session_clients_json_web_key_set_id_check forbids a set on any of
// them. Resolving the organization through project_id (AIM-77) is what lets a
// legacy client opt into private_key_jwt at all; refusing them instead would
// strand every pre-migration client on shared-secret authentication until some
// unrelated backfill lands.
//
// The adoption is not a shortcut around tenancy, it is the point at which
// tenancy becomes knowable. The composite foreign key to json_web_key_sets is
// MATCH SIMPLE and skips its check entirely while organization_id is NULL, so
// the column has to be populated before the database can pin the set to an
// organization at all. BackfillRemoteSessionClientOrganization writes only when
// the client's project belongs to the caller's organization, which makes the
// statement its own ownership check: no rows means the organization could not
// be established, and the caller is refused rather than adopted.
//
// A client with no project and no organization is the platform-owned global
// tier. It matches nothing in the backfill and is refused, which is correct:
// those rows are not a tenant's to claim.
func adoptClientOrganization(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, client *repo.RemoteSessionClient, organizationID string) (bool, error) {
	if client.OrganizationID.Valid && client.OrganizationID.String != "" {
		if client.OrganizationID.String != organizationID {
			return false, oops.E(oops.CodeNotFound, nil, "remote session client not found").LogError(ctx, logger)
		}

		return false, nil
	}

	rows, err := txRepo.BackfillRemoteSessionClientOrganization(ctx, repo.BackfillRemoteSessionClientOrganizationParams{
		ID:             client.ID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "backfill remote session client organization").LogError(ctx, logger)
	}
	if rows == 0 {
		return false, oops.E(oops.CodeFailedPrecondition, nil, "this remote session client has no owning organization to resolve, so it cannot hold a key set")
	}

	// The statement only writes when the project resolves to this organization,
	// so the row now holds exactly it. Reflecting that here keeps the view and
	// the subsequent tenancy-scoped write consistent without a second read.
	client.OrganizationID = conv.ToPGText(organizationID)

	return true, nil
}

// requireDetachableKeySet enforces the outbound half of the rule the inbound
// client registration surface already enforces as
// "private_key_jwt requires jwks or jwks_uri" (usersessions/jwks.ValidateKeySource).
// A client that declares private_key_jwt authenticates by signing an assertion
// with a key from its set; detaching the set leaves it declaring a method it
// cannot execute, which fails at the counterparty's token endpoint as an
// opaque 401 rather than here.
//
// Unreachable until AIM-156 adds private_key_jwt to tokenEndpointAuthMethodEnum.
// That ordering is deliberate: the guard lands before the value it guards is
// selectable, so there is never a window where an administrator can choose a
// method Gram cannot execute.
func requireDetachableKeySet(client repo.RemoteSessionClient) error {
	if TokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String) != TokenEndpointAuthMethodPrivateKeyJWT {
		return nil
	}

	return oops.E(oops.CodeConflict, nil, "client authenticates with private_key_jwt and has nothing to sign with once the key set is detached; change token_endpoint_auth_method first")
}

// lockProjectClientForAuthMethodWrite and
// lockOrganizationClientForAuthMethodWrite pin the client row for the rest of
// the transaction, so the private_key_jwt coupling cannot be decided from a
// read another handler invalidates before either commits. Must run before the
// client is read, or the read it protects is the stale one.
//
// Each is scoped to its own surface's reachability rather than locking by id
// alone. An unscoped lock would let any authenticated caller take a row lock on
// another tenant's client for the length of a transaction before the ownership
// check rejects them. A missing row is left to the caller's own read, which
// reports it as not found.
func lockProjectClientForAuthMethodWrite(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, clientID uuid.UUID, projectID uuid.UUID) error {
	_, err := txRepo.LockRemoteSessionClientForAuthMethodWrite(ctx, repo.LockRemoteSessionClientForAuthMethodWriteParams{
		ID:        clientID,
		ProjectID: conv.ToNullUUID(projectID),
	})

	return interpretClientLock(ctx, logger, err)
}

func lockOrganizationClientForAuthMethodWrite(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, clientID uuid.UUID, organizationID string) error {
	_, err := txRepo.LockOrganizationRemoteSessionClientForAuthMethodWrite(ctx, repo.LockOrganizationRemoteSessionClientForAuthMethodWriteParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(organizationID),
	})

	return interpretClientLock(ctx, logger, err)
}

func interpretClientLock(ctx context.Context, logger *slog.Logger, err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lock remote session client").LogError(ctx, logger)
	}

	return nil
}

// resolveAttachableKeySet takes the named set's row in FOR SHARE for the rest
// of the transaction, which is what serializes an attach against a concurrent
// deleteSet. The set must be live and in the client's organization; anything
// else reads as not found rather than leaking whether a set exists elsewhere.
func resolveAttachableKeySet(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, keySetID uuid.UUID, organizationID string) error {
	_, err := txRepo.LockJsonWebKeySetForClientAttach(ctx, repo.LockJsonWebKeySetForClientAttachParams{
		ID:             keySetID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "json web key set not found").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lock json web key set").LogError(ctx, logger)
	}

	return nil
}

// AttachKeySet attaches an organization JSON Web Key Set to a project-tier
// remote_session_client, opting it into signing private_key_jwt assertions.
func (s *Service) AttachKeySet(ctx context.Context, payload *clientsgen.AttachKeySetPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := s.requireKeySetEntitlement(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	clientID, keySetID, err := parseKeySetAttachIDs(ctx, logger, payload.ID, payload.JSONWebKeySetID)
	if err != nil {
		return nil, err
	}

	return s.mutateProjectClientKeySet(ctx, logger, *authCtx, clientID, uuid.NullUUID{UUID: keySetID, Valid: true})
}

// DetachKeySet clears the JSON Web Key Set on a project-tier
// remote_session_client. Detaching when no set is attached is a no-op.
func (s *Service) DetachKeySet(ctx context.Context, payload *clientsgen.DetachKeySetPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := s.requireKeySetEntitlement(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	return s.mutateProjectClientKeySet(ctx, logger, *authCtx, clientID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
}

// mutateProjectClientKeySet applies an attach (valid target) or a detach
// (invalid target) on the project surface.
//
// The client is resolved with an empty organization_id, which keeps
// organization-level clients invisible here exactly as UpdateRemoteSessionClient
// does: an organization-level client is not mutable from the project surface,
// and an attempt against one resolves to a clean not-found. This deliberately
// departs from AttachUserSessionIssuer, which passes the real organization so a
// project admin can bind an org-level client to their own user_session_issuer.
// Binding an issuer is a project-local decision about a shared client; changing
// which key the shared client signs with is not, and would let one project
// re-key a client every other project in the organization also uses.
func (s *Service) mutateProjectClientKeySet(ctx context.Context, logger *slog.Logger, authCtx contextvalues.AuthContext, clientID uuid.UUID, target uuid.NullUUID) (*types.RemoteSessionClient, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := lockProjectClientForAuthMethodWrite(ctx, logger, txRepo, clientID, *authCtx.ProjectID); err != nil {
		return nil, err
	}

	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: conv.ToPGTextEmpty(""),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	settled, err := s.settleClientKeySet(ctx, logger, dbtx, txRepo, authCtx, existing.RemoteSessionClient, existing.UserSessionIssuerIds, target, func(ctx context.Context) (repo.RemoteSessionClient, error) {
		return txRepo.SetRemoteSessionClientJsonWebKeySet(ctx, repo.SetRemoteSessionClientJsonWebKeySetParams{
			JsonWebKeySetID: target,
			ID:              clientID,
			ProjectID:       conv.ToNullUUID(*authCtx.ProjectID),
			OrganizationID:  conv.ToPGText(authCtx.ActiveOrganizationID),
		})
	})
	if err != nil {
		return nil, err
	}

	return settled, nil
}

// AttachClientKeySet attaches an organization JSON Web Key Set to a
// remote_session_client from the organization-administrator surface.
func (s *Service) AttachClientKeySet(ctx context.Context, payload *orgclientsgen.AttachClientKeySetPayload) (*types.RemoteSessionClient, error) {
	authCtx, logger, err := s.requireOrgClientKeySetAccess(ctx)
	if err != nil {
		return nil, err
	}

	clientID, keySetID, err := parseKeySetAttachIDs(ctx, logger, payload.ID, payload.JSONWebKeySetID)
	if err != nil {
		return nil, err
	}

	return s.mutateOrganizationClientKeySet(ctx, logger, *authCtx, clientID, uuid.NullUUID{UUID: keySetID, Valid: true})
}

// DetachClientKeySet clears the JSON Web Key Set on a remote_session_client
// from the organization-administrator surface. A no-op when none is attached.
func (s *Service) DetachClientKeySet(ctx context.Context, payload *orgclientsgen.DetachClientKeySetPayload) (*types.RemoteSessionClient, error) {
	authCtx, logger, err := s.requireOrgClientKeySetAccess(ctx)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	return s.mutateOrganizationClientKeySet(ctx, logger, *authCtx, clientID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
}

func (s *Service) requireOrgClientKeySetAccess(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}

	if err := s.requireKeySetEntitlement(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, logger, err
	}

	return authCtx, logger, nil
}

func (s *Service) mutateOrganizationClientKeySet(ctx context.Context, logger *slog.Logger, authCtx contextvalues.AuthContext, clientID uuid.UUID, target uuid.NullUUID) (*types.RemoteSessionClient, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := lockOrganizationClientForAuthMethodWrite(ctx, logger, txRepo, clientID, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	existing, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	return s.settleClientKeySet(ctx, logger, dbtx, txRepo, authCtx, existing.RemoteSessionClient, existing.UserSessionIssuerIds, target, func(ctx context.Context) (repo.RemoteSessionClient, error) {
		return txRepo.SetOrganizationRemoteSessionClientJsonWebKeySet(ctx, repo.SetOrganizationRemoteSessionClientJsonWebKeySetParams{
			JsonWebKeySetID: target,
			ID:              clientID,
			OrganizationID:  conv.ToPGText(authCtx.ActiveOrganizationID),
		})
	})
}

// settleClientKeySet holds every rule both surfaces share: eligibility, the
// private_key_jwt coupling, the set lock, the write, and the audit entry. The
// surfaces differ only in how they resolved the client and which write query
// they run, which is what the write closure carries.
func (s *Service) settleClientKeySet(
	ctx context.Context,
	logger *slog.Logger,
	dbtx pgx.Tx,
	txRepo *repo.Queries,
	authCtx contextvalues.AuthContext,
	existing repo.RemoteSessionClient,
	userSessionIssuerIDs []uuid.UUID,
	target uuid.NullUUID,
	write func(ctx context.Context) (repo.RemoteSessionClient, error),
) (*types.RemoteSessionClient, error) {
	// Re-applying the state the row already holds changes nothing, so it neither
	// writes nor audits, in either direction. An audit entry for a change that
	// did not happen is a false record, and a dashboard replaying its own
	// optimistic state should not inflate the log. Checked before adoption so a
	// no-op detach never rewrites a client's organization as a side effect.
	if sameKeySet(existing.JsonWebKeySetID, target) {
		view, err := mv.BuildRemoteSessionClientView(existing, userSessionIssuerIDs)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
		}
		return view, nil
	}

	var adopted bool
	if target.Valid {
		var err error
		adopted, err = adoptClientOrganization(ctx, logger, txRepo, &existing, authCtx.ActiveOrganizationID)
		if err != nil {
			return nil, err
		}

		if err := resolveAttachableKeySet(ctx, logger, txRepo, target.UUID, authCtx.ActiveOrganizationID); err != nil {
			return nil, err
		}
	} else if err := requireDetachableKeySet(existing); err != nil {
		// Detaching needs no organization: the CHECK constraint guarantees a
		// client with a NULL organization_id holds no set, so it took the no-op
		// branch above rather than reaching here.
		return nil, err
	}

	updated, err := write(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "set remote session client json web key set").LogError(ctx, logger)
	}

	view, err := mv.BuildRemoteSessionClientView(updated, userSessionIssuerIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}

	// On a detach the recorded set is the one that was removed, so the entry
	// stands on its own without joining back to the attach that preceded it.
	recorded := target
	if !recorded.Valid {
		recorded = existing.JsonWebKeySetID
	}

	event := audit.LogRemoteSessionClientKeySetAttachmentEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(updated.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
		ClientID:               updated.ClientID,
		JsonWebKeySetURN:       urn.NewJsonWebKeySet(recorded.UUID),
		AdoptedOrganization:    adopted,
	}

	logKeySetChange := s.auditLogger.LogRemoteSessionClientDetachKeySet
	if target.Valid {
		logKeySetChange = s.auditLogger.LogRemoteSessionClientAttachKeySet
	}

	if err := logKeySetChange(ctx, dbtx, event); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client key set change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return view, nil
}

func parseKeySetAttachIDs(ctx context.Context, logger *slog.Logger, rawClientID string, rawKeySetID string) (uuid.UUID, uuid.UUID, error) {
	clientID, err := uuid.Parse(rawClientID)
	if err != nil {
		return uuid.Nil, uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	keySetID, err := uuid.Parse(rawKeySetID)
	if err != nil {
		return uuid.Nil, uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid json_web_key_set_id").LogError(ctx, logger)
	}

	return clientID, keySetID, nil
}

// requirePrivateKeyJWTKeySet is the converse of requireDetachableKeySet: a
// client cannot declare private_key_jwt without a set to sign with, in the same
// way that requireDetachableKeySet stops the set being taken away afterwards.
//
// Neither rule is sufficient on its own, and neither is sufficient together
// without a lock: each reads a column the other writes, so two concurrent
// transactions can both pass against the same starting row and commit a client
// that declares private_key_jwt with no set. Every caller of either rule takes
// LockRemoteSessionClientForAuthMethodWrite on the client row first, which is
// what actually makes the pair hold.
//
// existing is the set the client already holds. On a create that is always the
// zero value, because a client is born without one: the link is attached
// through attachKeySet after the fact, so declaring private_key_jwt up front is
// always a refusal. On a global (platform-admin) client it is also always the
// zero value, permanently — those rows carry a NULL organization_id by
// construction and remote_session_clients_json_web_key_set_id_check forbids a
// set without one.
//
// Unreachable until AIM-156 adds private_key_jwt to tokenEndpointAuthMethodEnum;
// Goa rejects the value at the boundary until then. See
// TokenEndpointAuthMethodPrivateKeyJWT.
func requirePrivateKeyJWTKeySet(method *string, existing uuid.NullUUID) error {
	if method == nil || TokenEndpointAuthMethod(*method) != TokenEndpointAuthMethodPrivateKeyJWT {
		return nil
	}

	if existing.Valid {
		return nil
	}

	return oops.E(oops.CodeConflict, nil, "private_key_jwt requires an attached JSON Web Key Set; attach one before selecting this authentication method")
}

// sameKeySet reports whether the row already holds the requested state, for
// either direction: an attach of the set already attached, or a detach of a
// client that holds none.
func sameKeySet(current uuid.NullUUID, target uuid.NullUUID) bool {
	if !target.Valid {
		return !current.Valid
	}

	return current.Valid && current.UUID == target.UUID
}
