package remotesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// orgDisplayName mirrors the dashboard's formatRemoteMcpDisplay(): prefer a
// trimmed name, else fall back to the (protocol-bearing) URL.
func orgDisplayName(name *string, url string) string {
	if name != nil {
		if trimmed := strings.TrimSpace(*name); trimmed != "" {
			return trimmed
		}
	}
	return url
}

// CreateIssuer creates an issuer in the caller's organization. With no
// project_id the issuer is organization-level (project_id NULL); with a
// project_id (validated to belong to the org) it is project-specific. Gated on
// org:admin so org admins can provision both kinds without project-scoped grants.
func (s *Service) CreateIssuer(ctx context.Context, payload *orgissuersgen.CreateIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if strings.TrimSpace(payload.Slug) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").LogError(ctx, logger)
	}
	if strings.TrimSpace(payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	// Operator-supplied and later rendered as a link, so it is validated here.
	// An empty value stays legal: the create query stores it as NULL.
	if v := conv.PtrValOr(payload.ClientSetupDocumentationURL, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_setup_documentation_url must be an absolute http(s) URL").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	// Discovery drops malformed documentation URLs, but a caller holding the write
	// scope can POST them without ever calling discover, and they are persisted
	// and later rendered as links. An empty value stays legal: the update queries
	// read it as the explicit "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ServiceDocumentation, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "service_documentation must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpPolicyURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_policy_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpTosURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_tos_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}

	// Resolve the optional owning project. A provided project must belong to the
	// caller's organization; otherwise the issuer is organization-level.
	projectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	auditProjectID := uuid.Nil
	if payload.ProjectID != nil && strings.TrimSpace(*payload.ProjectID) != "" {
		pid, perr := uuid.Parse(*payload.ProjectID)
		if perr != nil {
			return nil, oops.E(oops.CodeBadRequest, perr, "invalid project id").LogError(ctx, logger)
		}
		if _, perr := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
			ID:             pid,
			OrganizationID: authCtx.ActiveOrganizationID,
		}); perr != nil {
			if errors.Is(perr, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, perr, "project not found in organization").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, perr, "validate project").LogError(ctx, logger)
		}
		projectID = conv.ToNullUUID(pid)
		auditProjectID = pid
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         projectID,
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              payload.Slug,
		Issuer:                            payload.Issuer,
		Name:                              conv.PtrToPGTextTrimmed(payload.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGTextEmpty(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGTextEmpty(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGTextEmpty(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGTextEmpty(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrValOr(payload.ClientIDMetadataDocumentSupported, false),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
		if isRemoteSessionIssuerSlugConflict(err) || isGlobalRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "an issuer with this slug already exists").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create organization admin remote session issuer").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionIssuerCreate(ctx, dbtx, audit.LogRemoteSessionIssuerCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              auditProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(issuer.ID),
		Slug:                   issuer.Slug,
		IssuerURL:              issuer.Issuer,
		Name:                   conv.FromPGText[string](issuer.Name),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session issuer creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// ListIssuers lists every remote_session_issuer in the caller's
// organization — organizational and project-specific — with associated client
// counts and, for project-specific issuers, the owning project name.
func (s *Service) ListIssuers(ctx context.Context, payload *orgissuersgen.ListIssuersPayload) (*orgissuersgen.ListOrganizationRemoteSessionIssuersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListOrganizationRemoteSessionIssuers(ctx, repo.ListOrganizationRemoteSessionIssuersParams{
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:         cursor,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization admin remote session issuers").LogError(ctx, s.logger)
	}

	items := make([]*orgissuersgen.OrganizationRemoteSessionIssuer, 0, len(rows))
	for _, row := range rows {
		items = append(items, &orgissuersgen.OrganizationRemoteSessionIssuer{
			Issuer:      mv.BuildRemoteSessionIssuerView(row.RemoteSessionIssuer),
			ClientCount: int(row.ClientCount),
			ProjectName: conv.PtrEmpty(row.ProjectName),
		})
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSessionIssuer.ID.String()
		nextCursor = &c
	}

	return &orgissuersgen.ListOrganizationRemoteSessionIssuersResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetIssuer resolves any issuer (organizational or project-specific) in the
// caller's organization by id.
func (s *Service) GetIssuer(ctx context.Context, payload *orgissuersgen.GetIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	issuer, err := repo.New(s.db).GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// GetIssuerDeletePreflight returns the authoritative impact of deleting an
// issuer: client count and the names of MCP servers its clients are attached to.
func (s *Service) GetIssuerDeletePreflight(ctx context.Context, payload *orgissuersgen.GetIssuerDeletePreflightPayload) (*orgissuersgen.OrganizationIssuerDeletePreflight, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	if _, err := r.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	clientCount, err := r.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count remote session clients").LogError(ctx, logger)
	}

	nameRows, err := r.ListOrganizationMcpServerNamesForIssuer(ctx, issuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp server names for issuer").LogError(ctx, logger)
	}

	names := make([]string, 0, len(nameRows))
	for _, row := range nameRows {
		names = append(names, orgDisplayName(conv.FromPGText[string](row.Name), row.Url))
	}

	return &orgissuersgen.OrganizationIssuerDeletePreflight{
		ClientCount:    int(clientCount),
		McpServerNames: names,
	}, nil
}

// UpdateIssuer patches any issuer in the caller's organization.
func (s *Service) UpdateIssuer(ctx context.Context, payload *orgissuersgen.UpdateIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

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

	// Discovery drops malformed documentation URLs, but a caller holding the write
	// scope can POST them without ever calling discover, and they are persisted
	// and later rendered as links. An empty value stays legal: the update queries
	// read it as the explicit "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ServiceDocumentation, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "service_documentation must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpPolicyURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_policy_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpTosURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_tos_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// Operator-supplied and later rendered as a link, so it is validated here.
	// An empty value stays legal: the update query reads it as the explicit
	// "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ClientSetupDocumentationURL, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_setup_documentation_url must be an absolute http(s) URL").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(existing)

	updated, err := txRepo.UpdateOrganizationRemoteSessionIssuer(ctx, repo.UpdateOrganizationRemoteSessionIssuerParams{
		Slug:                              conv.PtrToPGText(payload.Slug),
		Issuer:                            conv.PtrToPGText(payload.Issuer),
		Name:                              conv.PtrToPGText(payload.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGText(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGText(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGText(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGText(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrToPGBool(payload.ClientIDMetadataDocumentSupported),
		Oidc:                              conv.PtrToPGBool(payload.Oidc),
		Passthrough:                       conv.PtrToPGBool(payload.Passthrough),
		ID:                                issuerID,
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update organization admin remote session issuer").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteSessionIssuerView(updated)

	if err := s.auditLogger.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(updated.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(updated.ID),
		Slug:                   updated.Slug,
		IssuerURL:              updated.Issuer,
		Name:                   conv.FromPGText[string](updated.Name),
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session issuer update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

// DeleteIssuer soft-deletes any issuer in the caller's organization, blocked
// when clients still reference it.
func (s *Service) DeleteIssuer(ctx context.Context, payload *orgissuersgen.DeleteIssuerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Establish org ownership of the issuer before counting clients or deleting,
	// so a cross-org id returns NotFound rather than probing client counts or
	// silently succeeding against the org-scoped delete below.
	if _, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	clientCount, err := txRepo.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count remote session clients").LogError(ctx, logger)
	}
	if clientCount > 0 {
		return oops.E(oops.CodeConflict, nil, "remote session issuer has active clients; delete the clients first").LogError(ctx, logger)
	}

	deleted, err := txRepo.DeleteOrganizationRemoteSessionIssuer(ctx, repo.DeleteOrganizationRemoteSessionIssuerParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete organization admin remote session issuer").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionIssuerDelete(ctx, dbtx, audit.LogRemoteSessionIssuerDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(deleted.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(deleted.ID),
		Slug:                   deleted.Slug,
		IssuerURL:              deleted.Issuer,
		Name:                   conv.FromPGText[string](deleted.Name),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization admin remote session issuer deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// MoveIssuer re-scopes any issuer in the caller's organization: a provided
// project_id (validated to belong to the org) makes it project-specific; an
// absent project_id makes it organization-level (project_id NULL).
func (s *Service) MoveIssuer(ctx context.Context, payload *orgissuersgen.MoveIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// Resolve the optional target project. A provided project must belong to the
	// caller's organization; an absent one clears project_id to organization-level.
	projectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.ProjectID != nil && strings.TrimSpace(*payload.ProjectID) != "" {
		pid, perr := uuid.Parse(*payload.ProjectID)
		if perr != nil {
			return nil, oops.E(oops.CodeBadRequest, perr, "invalid project id").LogError(ctx, logger)
		}
		if _, perr := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
			ID:             pid,
			OrganizationID: authCtx.ActiveOrganizationID,
		}); perr != nil {
			if errors.Is(perr, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, perr, "project not found in organization").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, perr, "validate project").LogError(ctx, logger)
		}
		projectID = conv.ToNullUUID(pid)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(existing)

	updated, err := txRepo.SetOrganizationRemoteSessionIssuerProject(ctx, repo.SetOrganizationRemoteSessionIssuerProjectParams{
		ProjectID:      projectID,
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if isRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "an issuer with this slug already exists in the target project; rename the slug first").LogError(ctx, logger)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "move organization admin remote session issuer").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteSessionIssuerView(updated)

	if err := s.auditLogger.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(updated.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(updated.ID),
		Slug:                   updated.Slug,
		IssuerURL:              updated.Issuer,
		Name:                   conv.FromPGText[string](updated.Name),
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session issuer move").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

// orgProjectID flattens a nullable project id for audit events; org-level
// rows (NULL project_id) audit with uuid.Nil.
func orgProjectID(id uuid.NullUUID) uuid.UUID {
	if id.Valid {
		return id.UUID
	}
	return uuid.Nil
}
