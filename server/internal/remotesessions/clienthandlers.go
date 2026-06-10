package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	userIssuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
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

	// Reject the remote session issuer if it isn't reachable from the caller's
	// project, so a client can't be attached to another tenant's issuer. The
	// lookup accepts both the project's own issuers and organization-level
	// issuers.
	if _, err = txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID: conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
	}

	// Reject the user session issuer if it belongs to a different project, so
	// the binding can't cross a tenant boundary.
	if _, err = txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:        userIssuerID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerID:     userIssuerID,
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

	if err = txRepo.AttachRemoteSessionClientToUserSessionIssuer(
		ctx,
		repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: created.ID,
			UserSessionIssuerID:   userIssuerID,
		},
	); err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to attach remote session client to user session issuer",
		).LogError(ctx, logger)
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

	view, err := mv.BuildRemoteSessionClientView(created)
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

	// Prevent binding in the event that the issuer does not belong to the
	// current project.
	if _, err := txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:        userIssuerID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	encrypted, err := s.enc.Encrypt([]byte(clientSecret))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt client secret").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerID:     userIssuerID,
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

	if err := txRepo.AttachRemoteSessionClientToUserSessionIssuer(
		ctx,
		repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: created.ID,
			UserSessionIssuerID:   userIssuerID,
		},
	); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to attach remote session client to user session issuer").LogError(ctx, logger)
	}

	// Lift the legacy dynamic client registrations (Redis) for every MCP
	// server attached to this proxy provider into user_session_clients, on
	// the same transaction: a failure here aborts the whole clone so a
	// partial migration never commits.
	migrated, err := s.migrateLegacyClientRegistrations(ctx, txRepo, *authCtx.ProjectID, provider.OauthProxyServerID, userIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate legacy client registrations").LogError(ctx, logger)
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
		attr.SlogUserSessionIssuerID(userIssuerID.String()),
		attr.SlogUserSessionClientMigratedCount(migrated),
	)

	view, err := mv.BuildRemoteSessionClientView(created)
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

	userIssuerID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:        clientID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	beforeView, err := mv.BuildRemoteSessionClientView(existing)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	if payload.UserSessionIssuerID != nil {
		// Prevent binding in the event that the issuer does not belong to the
		// current project.
		if _, err := txRepo.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
			ID:        userIssuerID.UUID,
			ProjectID: *authCtx.ProjectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
		}
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
		UserSessionIssuerID:     userIssuerID,
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

	shouldRemakeUserSessionIssuerAttachment := payload.UserSessionIssuerID != nil && userIssuerID.Valid && userIssuerID.UUID != existing.UserSessionIssuerID

	if shouldRemakeUserSessionIssuerAttachment {
		// Deleting all attachments is a temporary measure to maintain
		// 1:1 relationship functionality while in this opportunistic backfill phase.
		if err = txRepo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(
			ctx,
			repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{
				RemoteSessionClientID: updated.ID,
				ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
			},
		); err != nil {
			return nil, oops.E(
				oops.CodeUnexpected,
				err,
				"failed to delete user session issuer attachments for remote session client %s",
				updated.ID,
			).LogError(ctx, logger)
		}

		if err = txRepo.AttachRemoteSessionClientToUserSessionIssuer(
			ctx,
			repo.AttachRemoteSessionClientToUserSessionIssuerParams{
				RemoteSessionClientID: updated.ID,
				UserSessionIssuerID:   updated.UserSessionIssuerID,
			},
		); err != nil {
			return nil, oops.E(
				oops.CodeUnexpected,
				err,
				"failed to attach remote session client to user session issuer",
			).LogError(ctx, logger)
		}
	}

	afterView, err := mv.BuildRemoteSessionClientView(updated)
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

	rows, err := s.listRemoteSessionClientsByProjectID(ctx, *authCtx.ProjectID, issuerFilter, userIssuerFilter, cursor, limit)
	if err != nil {
		if isRemoteSessionClientIssuerDrift(err) {
			return nil, oops.E(oops.CodeInvariantViolation, err, "multiple remote session clients found for user session issuer").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		item, err := mv.BuildRemoteSessionClientView(row)
		if err != nil {
			return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
		}
		items = append(items, item)
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
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
		ID:        clientID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}

	view, err := mv.BuildRemoteSessionClientView(client)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
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

	if err := txRepo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(
		ctx,
		repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{
			RemoteSessionClientID: deleted.ID,
			ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		},
	); err != nil {
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
