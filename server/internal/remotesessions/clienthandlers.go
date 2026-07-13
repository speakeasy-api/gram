package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// guardSingleClientPerRemoteIssuer enforces, at attach time, that at most one
// active remote_session_client is bound to a given (user_session_issuer,
// remote_session_issuer) pair through the join table. It scopes the
// constraint per remote_session_issuer, so a user_session_issuer can bind
// distinct clients across distinct remote issuers; the
// remote_session_client_user_session_issuers one_per_issuer index applies a
// stricter cap (one client per user_session_issuer regardless of remote
// issuer) until AIS-137 removes it, after which this guard is the sole
// attach-time enforcement.
//
// excludeClientID skips a row so an update of the same client passes; pass
// uuid.Nil to exclude nothing (the create paths). Must run inside the attach
// transaction. No database constraint enforces the per-pair uniqueness, so a
// narrow window remains between concurrent attaches; the runtime resolver's
// invariant (ResolveAccessTokens) is the backstop that surfaces any drift at
// serve time.
//
// organizationID lets the conflict scan see organization-level clients
// (project_id NULL) already bound to the user_session_issuer, so an org-level
// and a project-scoped client cannot both bind the same remote issuer to one
// user_session_issuer.
func (s *Service) guardSingleClientPerRemoteIssuer(
	ctx context.Context,
	logger *slog.Logger,
	txRepo *repo.Queries,
	organizationID string,
	projectID, userSessionIssuerID, remoteSessionIssuerID, excludeClientID uuid.UUID,
) error {
	// Two rows are enough to detect a conflict: at most one row can be
	// excludeClientID, so a second row guarantees another client is already
	// bound to the pair.
	bound, err := txRepo.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		UserSessionIssuerID:   userSessionIssuerID,
		ProjectID:             projectID,
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: uuid.NullUUID{UUID: remoteSessionIssuerID, Valid: true},
		Cursor:                uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:            2,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list remote session clients for user/remote issuer").LogError(ctx, logger)
	}
	for _, c := range bound {
		if c.RemoteSessionClient.ID != excludeClientID {
			return oops.E(oops.CodeConflict, nil, "a remote session client is already bound to this user session issuer for the same remote session issuer").LogError(ctx, logger)
		}
	}
	return nil
}

// parseUserSessionIssuerIDs parses and de-duplicates the user_session_issuer id
// strings from a create/clone form into a slice sorted by id. The sort matches
// the ORDER BY in the join-table read queries so a freshly created client's
// returned view lists its attachments in the same order a later read would.
func parseUserSessionIssuerIDs(raw []string) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]struct{}, len(raw))
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("parse user_session_issuer_id %q: %w", s, err)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids, nil
}

func (s *Service) CreateRemoteSessionClient(ctx context.Context, payload *gen.CreateRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	userIssuerIDs, err := parseUserSessionIssuerIDs(payload.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_ids").LogError(ctx, logger)
	}

	clientID := strings.TrimSpace(payload.ClientID)
	if clientID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id is required").LogError(ctx, logger)
	}

	var secretCiphertext pgtype.Text
	if payload.ClientSecret != nil && *payload.ClientSecret != "" {
		encrypted, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(encrypted)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if _, err := s.validateNewClientIssuers(ctx, logger, txRepo, *authCtx.ProjectID, authCtx.ActiveOrganizationID, issuerID, userIssuerIDs); err != nil {
		return nil, err
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuerID,
		ClientID:                clientID,
		ClientSecretEncrypted:   secretCiphertext,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		LegacyCallbackUrl:       false,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create remote session client").LogError(ctx, logger)
	}

	return s.finalizeClientCreate(ctx, logger, dbtx, txRepo, *authCtx, created, userIssuerIDs)
}

// CreateCimd registers a remote_session_client in Client ID Metadata Document
// (CIMD) mode. Unlike the manual create path the caller supplies no client_id
// or secret: Gram generates the row id, derives the platform-canonical document
// URL from it, and writes that URL as both client_id and client_id_metadata_uri
// in a single INSERT (token_endpoint_auth_method none, no secret). The owning
// issuer must advertise client_id_metadata_document support.
func (s *Service) CreateCimd(ctx context.Context, payload *gen.CreateCimdPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	userIssuerIDs, err := parseUserSessionIssuerIDs(payload.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_ids").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := s.validateNewClientIssuers(ctx, logger, txRepo, *authCtx.ProjectID, authCtx.ActiveOrganizationID, issuerID, userIssuerIDs)
	if err != nil {
		return nil, err
	}

	// Pre-flight against the issuer's discovered capabilities so an unsupported
	// pairing fails at create time, not at the first outbound call.
	if err := preflightCIMDIssuer(issuer); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "issuer does not support client id metadata documents").LogError(ctx, logger)
	}

	// Generate the id up front so the document URL (which embeds it) can be the
	// client_id on a single INSERT. uuid.NewV7 preserves the time-ordered shape
	// the id cursor pagination relies on, matching the DB default generate_uuidv7().
	clientID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate client id").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ID:                    clientID,
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: issuerID,
		ClientIDMetadataUri:   ClientMetadataDocumentURL(s.serverURL, clientID),
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		Scope:                 payload.Scope,
		Audience:              conv.PtrToPGText(payload.Audience),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create remote session client").LogError(ctx, logger)
	}

	return s.finalizeClientCreate(ctx, logger, dbtx, txRepo, *authCtx, created, userIssuerIDs)
}

// validateNewClientIssuers checks the remote_session_issuer is reachable from
// the caller's project and that every user_session_issuer belongs to the
// project and is not already bound to another client for the same remote
// issuer. Returns the issuer row so callers that need its capabilities (the
// CIMD pre-flight) can use it. Must run inside the create transaction.
func (s *Service) validateNewClientIssuers(
	ctx context.Context,
	logger *slog.Logger,
	txRepo *repo.Queries,
	projectID uuid.UUID,
	organizationID string,
	issuerID uuid.UUID,
	userIssuerIDs []uuid.UUID,
) (repo.RemoteSessionIssuer, error) {
	// The lookup accepts both the project's own issuers and organization-level
	// issuers, so a client can't be attached to another tenant's issuer.
	issuer, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      conv.ToNullUUID(projectID),
		OrganizationID: conv.ToPGTextEmpty(organizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.RemoteSessionIssuer{}, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return repo.RemoteSessionIssuer{}, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
	}

	// Reject any user session issuer that belongs to a different project (so a
	// binding can't cross a tenant boundary) and any pairing that would put a
	// second client on the same (user_session_issuer, remote_session_issuer)
	// pair. Validate every issuer before creating the row so a bad request never
	// leaves a half-attached client behind.
	for _, userIssuerID := range userIssuerIDs {
		if _, err := txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
			ID:        userIssuerID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return repo.RemoteSessionIssuer{}, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
			}
			return repo.RemoteSessionIssuer{}, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
		}

		if err := s.guardSingleClientPerRemoteIssuer(ctx, logger, txRepo, organizationID, projectID, userIssuerID, issuerID, uuid.Nil); err != nil {
			return repo.RemoteSessionIssuer{}, err
		}
	}

	return issuer, nil
}

// finalizeClientCreate binds a freshly created client to each
// user_session_issuer, records the create audit event, commits the
// transaction, and returns the API view. Shared by the manual and CIMD create
// paths, which differ only in how the row is inserted.
func (s *Service) finalizeClientCreate(
	ctx context.Context,
	logger *slog.Logger,
	dbtx pgx.Tx,
	txRepo *repo.Queries,
	authCtx contextvalues.AuthContext,
	created repo.RemoteSessionClient,
	userIssuerIDs []uuid.UUID,
) (*types.RemoteSessionClient, error) {
	for _, userIssuerID := range userIssuerIDs {
		if err := txRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: created.ID,
			UserSessionIssuerID:   userIssuerID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach remote session client to user session issuer").LogError(ctx, logger)
		}
	}

	if err := s.auditLogger.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(created.ID),
		ClientID:               created.ClientID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	view, err := mv.BuildRemoteSessionClientView(created, userIssuerIDs)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
}

// CloneClientFromOAuthProxyProvider mints a remote_session_client by lifting
// the client_id / client_secret out of an existing oauth_proxy_provider row.
// The upstream secret never leaves the server: it's read from the proxy
// provider's JSONB secrets, re-encrypted with the project encryption key, and
// persisted on the new client row. Restricted to platform admins (Gram-staff
// `users.admin` flag, the same gate sessions.go uses for cross-org access)
// so a customer operator can't trigger an unprompted credential migration
// from the dashboard. Customer admins run this from the dashboard via a
// platform-admin override path.
//
// Only "custom" proxy providers carry inline client_id / client_secret values;
// "gram"-type providers use the Gram-managed authorization URL and have no
// reusable upstream client to clone.
func (s *Service) CloneClientFromOAuthProxyProvider(ctx context.Context, payload *gen.CloneClientFromOAuthProxyProviderPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if !authCtx.IsAdmin {
		return nil, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	proxyProviderID, err := uuid.Parse(payload.OauthProxyProviderID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid oauth_proxy_provider_id").LogError(ctx, logger)
	}

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	userIssuerIDs, err := parseUserSessionIssuerIDs(payload.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_ids").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	provider, err := txRepo.GetOAuthProxyProviderForClone(ctx, repo.GetOAuthProxyProviderForCloneParams{
		ID:        proxyProviderID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "oauth proxy provider not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get oauth proxy provider").LogError(ctx, logger)
	}

	if provider.ProviderType != "custom" {
		return nil, oops.E(oops.CodeBadRequest, nil, "only custom oauth_proxy_providers carry a clonable client; provider_type=%q", provider.ProviderType).LogError(ctx, logger)
	}

	clientID, clientSecret, err := resolveProxyClientCredentials(ctx, s.environments, provider.ProjectID, provider.Secrets)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "oauth proxy provider client credentials unavailable for clone").LogError(ctx, logger)
	}

	// Confirm the issuer the caller named is reachable from the caller's
	// project — either an issuer the project owns or an organization-level
	// issuer inherited from the project's org — so a clone cannot graft a
	// client onto an unrelated tenant's issuer. The cloned client row is still
	// owned by the caller's project regardless of the issuer's scope; this
	// mirrors the reachability gate in CreateRemoteSessionClient.
	if _, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID: conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
	}

	// Prevent binding in the event that any issuer does not belong to the
	// current project, and reject a pairing that would put a second client on
	// the same (user_session_issuer, remote_session_issuer) pair. Validate every
	// issuer before creating the row.
	for _, userIssuerID := range userIssuerIDs {
		if _, err := txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
			ID:        userIssuerID,
			ProjectID: *authCtx.ProjectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
		}

		if err := s.guardSingleClientPerRemoteIssuer(ctx, logger, txRepo, authCtx.ActiveOrganizationID, *authCtx.ProjectID, userIssuerID, issuerID, uuid.Nil); err != nil {
			return nil, err
		}
	}

	encrypted, err := s.enc.Encrypt([]byte(clientSecret))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt client secret").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuerID,
		ClientID:                clientID,
		ClientSecretEncrypted:   conv.ToPGText(encrypted),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		// The cloned client_id is already registered upstream against the
		// oauth_proxy_servers /oauth/callback URL; the authorize leg has to
		// keep using that redirect_uri or the upstream's strict-match check
		// rejects the request.
		LegacyCallbackUrl: true,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create remote session client").LogError(ctx, logger)
	}

	// Attach the cloned client to each user_session_issuer and lift the legacy
	// dynamic client registrations (Redis) for every MCP server attached to this
	// proxy provider into user_session_clients, all on the same transaction: a
	// failure here aborts the whole clone so a partial migration never commits.
	var migrated int64
	for _, userIssuerID := range userIssuerIDs {
		if err := txRepo.AttachRemoteSessionClientToUserSessionIssuer(
			ctx,
			repo.AttachRemoteSessionClientToUserSessionIssuerParams{
				RemoteSessionClientID: created.ID,
				UserSessionIssuerID:   userIssuerID,
			},
		); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to attach remote session client to user session issuer").LogError(ctx, logger)
		}

		n, migErr := s.MigrateLegacyClientRegistrations(ctx, txRepo, *authCtx.ProjectID, provider.OauthProxyServerID, userIssuerID)
		if migErr != nil {
			return nil, oops.E(oops.CodeUnexpected, migErr, "migrate legacy client registrations").LogError(ctx, logger)
		}
		migrated += n
	}

	if err := s.auditLogger.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(created.ID),
		ClientID:               created.ClientID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logger.InfoContext(ctx, "cloned oauth proxy provider client",
		attr.SlogRemoteSessionClientID(created.ID.String()),
		attr.SlogUserSessionClientMigratedCount(migrated),
	)

	view, err := mv.BuildRemoteSessionClientView(created, userIssuerIDs)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
}

// resolveProxyClientCredentials pulls client_id and client_secret out of an
// oauth_proxy_provider's secrets JSONB, falling back to the linked environment
// when either field is missing. The fallback mirrors the runtime resolver in
// server/internal/oauth/providers/custom.go so cutover works for both inline
// and env-backed proxy providers. Environment values are loaded through the
// EnvironmentEntries helper, which decrypts them; case-insensitive matching
// reflects how operators name CLIENT_ID / CLIENT_SECRET in practice.
func resolveProxyClientCredentials(ctx context.Context, env *environments.EnvironmentEntries, projectID uuid.UUID, secretsJSON []byte) (clientID string, clientSecret string, err error) {
	if len(secretsJSON) == 0 {
		return "", "", fmt.Errorf("provider has no stored secrets")
	}
	var secrets map[string]string
	if err := json.Unmarshal(secretsJSON, &secrets); err != nil {
		return "", "", fmt.Errorf("decode provider secrets: %w", err)
	}
	clientID = strings.TrimSpace(secrets["client_id"])
	clientSecret = strings.TrimSpace(secrets["client_secret"])

	if envSlug := strings.TrimSpace(secrets["environment_slug"]); (clientID == "" || clientSecret == "") && envSlug != "" {
		if env == nil {
			return "", "", fmt.Errorf("provider references environment %q but environment loader is unavailable", envSlug)
		}
		envMap, loadErr := env.Load(ctx, projectID, toolconfig.Slug(envSlug))
		if loadErr != nil {
			return "", "", fmt.Errorf("load environment %q: %w", envSlug, loadErr)
		}
		for k, v := range envMap {
			switch strings.ToLower(k) {
			case "client_id":
				if clientID == "" {
					clientID = strings.TrimSpace(v)
				}
			case "client_secret":
				if clientSecret == "" {
					clientSecret = strings.TrimSpace(v)
				}
			}
		}
	}

	if clientID == "" {
		return "", "", fmt.Errorf("client_id is empty")
	}
	if clientSecret == "" {
		return "", "", fmt.Errorf("client_secret is empty")
	}
	return clientID, clientSecret, nil
}

func (s *Service) UpdateRemoteSessionClient(ctx context.Context, payload *gen.UpdateRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Project-only lookup: an organization-level client is not mutable from the
	// project surface, so passing an empty organization_id keeps org-level rows
	// invisible here and an update against one resolves to a clean not-found.
	// Org-level clients are edited through the org-admin update endpoint instead.
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

	// Issuer attachments are managed through attachUserSessionIssuer /
	// detachUserSessionIssuer, so an update never changes them; the same set
	// frames both the before and after views.
	beforeView, err := mv.BuildRemoteSessionClientView(existing.RemoteSessionClient, existing.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	var secretCiphertext pgtype.Text
	if payload.ClientSecret != nil && *payload.ClientSecret != "" {
		encrypted, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(encrypted)
	}

	updated, err := txRepo.UpdateRemoteSessionClient(ctx, repo.UpdateRemoteSessionClientParams{
		ClientSecretEncrypted:   secretCiphertext,
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		ID:                      clientID,
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote session client").LogError(ctx, logger)
	}

	afterView, err := mv.BuildRemoteSessionClientView(updated, existing.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
		ClientID:               updated.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) ListRemoteSessionClients(ctx context.Context, payload *gen.ListRemoteSessionClientsPayload) (*gen.ListRemoteSessionClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerFilter, err := conv.PtrToNullUUID(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	userIssuerFilter, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := s.listRemoteSessionClientsByProjectID(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, issuerFilter, userIssuerFilter, cursor, limit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		item, err := mv.BuildRemoteSessionClientView(row.Client, row.UserSessionIssuerIDs)
		if err != nil {
			return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
		}
		items = append(items, item)
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].Client.ID.String()
		nextCursor = &c
	}

	return &gen.ListRemoteSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetRemoteSessionClient(ctx context.Context, payload *gen.GetRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	client, err := repo.New(s.db).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	view, err := mv.BuildRemoteSessionClientView(client.RemoteSessionClient, client.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
}

// AttachUserSessionIssuer records a remote_session_client / user_session_issuer
// binding in the join table. The pairing is rejected when another client is
// already bound to the same user_session_issuer for this client's
// remote_session_issuer.
func (s *Service) AttachUserSessionIssuer(ctx context.Context, payload *gen.AttachUserSessionIssuerPayload) (*types.RemoteSessionClient, error) {
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Resolve the client (the project's own or an organization-level client in
	// the project's org) so a project admin can attach an org-level client to
	// their own user_session_issuer.
	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	// The user_session_issuer must belong to the caller's project so a binding
	// can't cross a tenant boundary.
	if _, err := txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:        userIssuerID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
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

	return s.commitClientAttachmentChange(ctx, logger, dbtx, txRepo, *authCtx, clientID, func(ctx context.Context, dbtx pgx.Tx) error {
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

// DetachUserSessionIssuer removes a remote_session_client / user_session_issuer
// binding from the join table. Detaching a binding that does not exist is a
// no-op.
func (s *Service) DetachUserSessionIssuer(ctx context.Context, payload *gen.DetachUserSessionIssuerPayload) (*types.RemoteSessionClient, error) {
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Resolve the client (the project's own or an organization-level client in
	// the project's org) before mutating the project-agnostic join table, so a
	// project admin can detach an org-level client from their own
	// user_session_issuer.
	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	// The user_session_issuer must belong to the caller's project. An org-level
	// client can be bound to user_session_issuers across projects in the same
	// org, so without this a project admin could detach another project's
	// binding through the (project-agnostic) join-table delete.
	if _, err := txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:        userIssuerID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	if _, err := txRepo.DetachRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachRemoteSessionClientFromUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "detach remote session client from user session issuer").LogError(ctx, logger)
	}

	return s.commitClientAttachmentChange(ctx, logger, dbtx, txRepo, *authCtx, clientID, func(ctx context.Context, dbtx pgx.Tx) error {
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

// commitClientAttachmentChange re-reads a client after an attach/detach, records
// the supplied attachment audit event on the same transaction, commits, and
// returns the after view. auditFn lets each caller emit the right action
// (attach vs detach) while the re-read/commit stays shared.
func (s *Service) commitClientAttachmentChange(
	ctx context.Context,
	logger *slog.Logger,
	dbtx pgx.Tx,
	txRepo *repo.Queries,
	authCtx contextvalues.AuthContext,
	clientID uuid.UUID,
	auditFn func(ctx context.Context, dbtx pgx.Tx) error,
) (*types.RemoteSessionClient, error) {
	updated, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	afterView, err := mv.BuildRemoteSessionClientView(updated.RemoteSessionClient, updated.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	if err := auditFn(ctx, dbtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client attachment change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

// DeleteRemoteSessionClient soft-deletes a client and cascades the soft-delete
// to the upstream-token rows pointing at it in the same transaction. The FK
// has ON DELETE CASCADE, but since we soft-delete the parent, dependent rows
// would otherwise stay reachable; force them out of any active set here.
func (s *Service) DeleteRemoteSessionClient(ctx context.Context, payload *gen.DeleteRemoteSessionClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{
		ID:        clientID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete remote session client").LogError(ctx, logger)
	}

	if err := txRepo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, deleted.ID); err != nil {
		return oops.E(
			oops.CodeUnexpected,
			err,
			"failed to delete user session issuer attachments for remote session client %s",
			deleted.ID,
		).LogError(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete dependent remote sessions").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDelete(ctx, dbtx, audit.LogRemoteSessionClientDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(deleted.ID),
		ClientID:               deleted.ClientID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session client deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}
