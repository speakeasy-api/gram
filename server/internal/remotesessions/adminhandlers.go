package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// The adminRemoteSessions handlers curate global remote_session_issuer /
// remote_session_client records (project_id NULL AND organization_id NULL),
// shared across every organization. No project/org exists to scope an RBAC
// grant, so each handler gates inline on the platform-admin flag; audit is
// structured-logs only (audit_log.organization_id is NOT NULL).

// requireGlobalAdmin extracts the auth context and enforces the platform-admin
// flag. The returned logger is pre-tagged with the actor for audit/error lines.
func (s *Service) requireGlobalAdmin(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogUserID(authCtx.UserID))

	if !authCtx.IsAdmin {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}

// orEmptySlice coalesces a nil slice to empty: the remote_session_issuers
// *_supported columns are NOT NULL, and an explicit NULL in the INSERT bypasses
// their empty-array default.
func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// logGlobalMutation records a structured-log audit line (actor, action,
// subject) for a global mutation, standing in for the auditlogs rows globals
// can't have.
func logGlobalMutation(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, action, subject, subjectID string) {
	logger.InfoContext(ctx, "global remote session "+subject+" "+action,
		attr.SlogAuditAction(action),
		attr.SlogAuditSubject(subject),
		attr.SlogAuditSubjectID(subjectID),
		attr.SlogAuthUserEmail(conv.PtrValOrEmpty(authCtx.Email, "")),
	)
}

// --- Global issuers ---

// CreateGlobalIssuer creates a global remote_session_issuer (project_id NULL,
// organization_id NULL), reusing CreateRemoteSessionIssuer with NULL scoping.
func (s *Service) CreateGlobalIssuer(ctx context.Context, payload *adminrsgen.CreateGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(payload.Slug) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").LogError(ctx, logger)
	}
	if strings.TrimSpace(payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	issuer, err := repo.New(dbtx).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                    pgtype.Text{String: "", Valid: false},
		Slug:                              payload.Slug,
		Issuer:                            payload.Issuer,
		Name:                              conv.PtrToPGTextTrimmed(payload.Name),
		LogoAssetID:                       logoAssetID,
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   orEmptySlice(payload.ScopesSupported),
		GrantTypesSupported:               orEmptySlice(payload.GrantTypesSupported),
		ResponseTypesSupported:            orEmptySlice(payload.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: orEmptySlice(payload.TokenEndpointAuthMethodsSupported),
		ClientIDMetadataDocumentSupported: conv.PtrValOr(payload.ClientIDMetadataDocumentSupported, false),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
		if isGlobalRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "a global issuer with this slug already exists").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create global remote session issuer").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "create", "issuer", issuer.ID.String())

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// ListGlobalIssuers lists the global remote_session_issuers.
func (s *Service) ListGlobalIssuers(ctx context.Context, payload *adminrsgen.ListGlobalIssuersPayload) (*adminrsgen.ListRemoteSessionIssuersResult, error) {
	_, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListGlobalRemoteSessionIssuers(ctx, repo.ListGlobalRemoteSessionIssuersParams{
		Cursor:     cursor,
		LimitValue: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list global remote session issuers").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSessionIssuer, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionIssuerView(row))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &adminrsgen.ListRemoteSessionIssuersResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetGlobalIssuer resolves a global remote_session_issuer by id.
func (s *Service) GetGlobalIssuer(ctx context.Context, payload *adminrsgen.GetGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	_, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	issuer, err := repo.New(s.db).GetGlobalRemoteSessionIssuerByID(ctx, issuerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// UpdateGlobalIssuer patches a global remote_session_issuer.
func (s *Service) UpdateGlobalIssuer(ctx context.Context, payload *adminrsgen.UpdateGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if payload.Slug != nil && *payload.Slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug cannot be set to empty").LogError(ctx, logger)
	}
	if payload.Issuer != nil && *payload.Issuer == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer cannot be set to empty").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := repo.New(dbtx).UpdateGlobalRemoteSessionIssuer(ctx, repo.UpdateGlobalRemoteSessionIssuerParams{
		Slug:                              conv.PtrToPGText(payload.Slug),
		Issuer:                            conv.PtrToPGText(payload.Issuer),
		Name:                              conv.PtrToPGText(payload.Name),
		LogoAssetID:                       logoAssetID,
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrToPGBool(payload.ClientIDMetadataDocumentSupported),
		Oidc:                              conv.PtrToPGBool(payload.Oidc),
		Passthrough:                       conv.PtrToPGBool(payload.Passthrough),
		ID:                                issuerID,
	})
	if err != nil {
		if isGlobalRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "a global issuer with this slug already exists").LogError(ctx, logger)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update global remote session issuer").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "update", "issuer", updated.ID.String())

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(updated), nil
}

// DeleteGlobalIssuer soft-deletes a global remote_session_issuer, blocked when
// any global clients still reference it (the operator deletes the clients
// first). Mirrors the org-scoped DeleteIssuer.
func (s *Service) DeleteGlobalIssuer(ctx context.Context, payload *adminrsgen.DeleteGlobalIssuerPayload) error {
	authCtx, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Establish the issuer is global before counting clients or deleting, so a
	// non-global id returns NotFound rather than probing client counts.
	if _, err := txRepo.GetGlobalRemoteSessionIssuerByID(ctx, issuerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	clientCount, err := txRepo.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count remote session clients").LogError(ctx, logger)
	}
	if clientCount > 0 {
		return oops.E(oops.CodeConflict, nil, "global remote session issuer has active clients; delete the clients first").LogError(ctx, logger)
	}

	deleted, err := txRepo.DeleteGlobalRemoteSessionIssuer(ctx, issuerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete global remote session issuer").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "delete", "issuer", deleted.ID.String())

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// --- Global clients ---

// CreateGlobalClient registers a global remote_session_client under an existing
// global issuer, reusing CreateRemoteSessionClient with NULL scoping. Global
// clients carry no user_session_issuer attachments.
func (s *Service) CreateGlobalClient(ctx context.Context, payload *adminrsgen.CreateGlobalClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	clientID := strings.TrimSpace(payload.ClientID)
	if clientID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id is required").LogError(ctx, logger)
	}

	// Encrypt a supplied client secret before it touches the database; an absent
	// secret leaves the stored ciphertext NULL.
	var secretCiphertext pgtype.Text
	if payload.ClientSecret != nil && *payload.ClientSecret != "" {
		ciphertext, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(ciphertext)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Reject an issuer that isn't global so a global client can't be registered
	// against a project- or org-scoped issuer.
	if _, err := txRepo.GetGlobalRemoteSessionIssuerByID(ctx, issuerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:          pgtype.Text{String: "", Valid: false},
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
		return nil, oops.E(oops.CodeUnexpected, err, "create global remote session client").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "create", "client", created.ID.String())

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildGlobalRemoteSessionClientView(created), nil
}

// ListGlobalClients lists the global clients registered with a global issuer.
func (s *Service) ListGlobalClients(ctx context.Context, payload *adminrsgen.ListGlobalClientsPayload) (*adminrsgen.ListRemoteSessionClientsResult, error) {
	_, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListGlobalRemoteSessionClientsByIssuerID(ctx, repo.ListGlobalRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: issuerID,
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list global remote session clients").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildGlobalRemoteSessionClientView(row))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &adminrsgen.ListRemoteSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetGlobalClient resolves a global client by id.
func (s *Service) GetGlobalClient(ctx context.Context, payload *adminrsgen.GetGlobalClientPayload) (*types.RemoteSessionClient, error) {
	_, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	client, err := repo.New(s.db).GetGlobalRemoteSessionClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session client").LogError(ctx, logger)
	}

	return mv.BuildGlobalRemoteSessionClientView(client), nil
}

// UpdateGlobalClient patches a global client's non-issuer fields, rotating the
// client secret when supplied.
func (s *Service) UpdateGlobalClient(ctx context.Context, payload *adminrsgen.UpdateGlobalClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	// Encrypt a rotated client secret before it touches the database; an absent
	// secret leaves the stored ciphertext untouched (narg NULL → COALESCE keeps).
	var clientSecretEncrypted pgtype.Text
	if payload.ClientSecret != nil {
		if strings.TrimSpace(*payload.ClientSecret) == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_secret cannot be empty").LogError(ctx, logger)
		}
		ciphertext, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		clientSecretEncrypted = conv.ToPGText(ciphertext)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := repo.New(dbtx).UpdateGlobalRemoteSessionClient(ctx, repo.UpdateGlobalRemoteSessionClientParams{
		ClientSecretEncrypted:   clientSecretEncrypted,
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		ID:                      clientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update global remote session client").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "update", "client", updated.ID.String())

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildGlobalRemoteSessionClientView(updated), nil
}

// DeleteGlobalClient soft-deletes a global client and cascades the
// remote_sessions minted against it.
func (s *Service) DeleteGlobalClient(ctx context.Context, payload *adminrsgen.DeleteGlobalClientPayload) error {
	authCtx, logger, err := s.requireGlobalAdmin(ctx)
	if err != nil {
		return err
	}

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

	deleted, err := txRepo.DeleteGlobalRemoteSessionClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete global remote session client").LogError(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete dependent remote sessions").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "delete", "client", deleted.ID.String())

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}
