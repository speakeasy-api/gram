package remotesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
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
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
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
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
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

// ListClients lists the clients registered with an issuer in the caller's
// organization, each with its MCP server attachment count.
func (s *Service) ListClients(ctx context.Context, payload *orgissuersgen.ListClientsPayload) (*orgissuersgen.ListOrganizationRemoteSessionClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.IssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListOrganizationRemoteSessionClientsByIssuerID(ctx, repo.ListOrganizationRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: issuerID,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization admin remote session clients").LogError(ctx, logger)
	}

	items := make([]*orgissuersgen.OrganizationRemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		clientView, err := mv.BuildRemoteSessionClientView(row.RemoteSessionClient)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
		}
		items = append(items, &orgissuersgen.OrganizationRemoteSessionClient{
			Client:             clientView,
			McpServerCount:     int(row.McpServerCount),
			ActiveSessionCount: int(row.ActiveSessionCount),
		})
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSessionClient.ID.String()
		nextCursor = &c
	}

	return &orgissuersgen.ListOrganizationRemoteSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetClient resolves a client in the caller's organization by id.
func (s *Service) GetClient(ctx context.Context, payload *orgissuersgen.GetClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	client, err := repo.New(s.db).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	view, err := mv.BuildRemoteSessionClientView(client)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}
	return view, nil
}

// GetClientDeletePreflight returns the authoritative impact of deleting a
// client: session count and the names of MCP servers it is attached to.
func (s *Service) GetClientDeletePreflight(ctx context.Context, payload *orgissuersgen.GetClientDeletePreflightPayload) (*orgissuersgen.OrganizationClientDeletePreflight, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	if _, err := r.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	sessionCount, err := r.CountActiveRemoteSessionsByClientID(ctx, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count remote sessions").LogError(ctx, logger)
	}

	mcpRows, err := r.ListOrganizationMcpServersForClient(ctx, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for client").LogError(ctx, logger)
	}

	names := make([]string, 0, len(mcpRows))
	for _, row := range mcpRows {
		names = append(names, orgDisplayName(conv.FromPGText[string](row.Name), row.Url))
	}

	return &orgissuersgen.OrganizationClientDeletePreflight{
		SessionCount:   int(sessionCount),
		McpServerNames: names,
	}, nil
}

// ListClientMcpServers lists the MCP servers a client is attached to in the
// caller's organization.
func (s *Service) ListClientMcpServers(ctx context.Context, payload *orgissuersgen.ListClientMcpServersPayload) (*orgissuersgen.ListOrganizationMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	// Establish org ownership of the client before resolving its MCP servers.
	if _, err := r.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	rows, err := r.ListOrganizationMcpServersForClient(ctx, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for client").LogError(ctx, logger)
	}

	items := make([]*orgissuersgen.OrganizationMcpServer, 0, len(rows))
	for _, row := range rows {
		items = append(items, &orgissuersgen.OrganizationMcpServer{
			ID:          row.ID.String(),
			ProjectID:   row.ProjectID.String(),
			ProjectSlug: conv.PtrEmpty(row.ProjectSlug),
			Name:        conv.FromPGText[string](row.Name),
			Slug:        conv.FromPGText[string](row.Slug),
			URL:         conv.PtrEmpty(row.Url),
		})
	}

	return &orgissuersgen.ListOrganizationMcpServersResult{Items: items}, nil
}

// ListClientSessions lists the sessions minted against a client in the
// caller's organization.
func (s *Service) ListClientSessions(ctx context.Context, payload *orgissuersgen.ListClientSessionsPayload) (*orgissuersgen.ListOrganizationRemoteSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListOrganizationRemoteSessionsByClientID(ctx, repo.ListOrganizationRemoteSessionsByClientIDParams{
		RemoteSessionClientID: clientID,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization admin remote sessions").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSession, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionView(row.RemoteSession, conv.FromPGText[string](row.SubjectDisplayName), conv.FromPGText[string](row.SubjectEmail)))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSession.ID.String()
		nextCursor = &c
	}

	return &orgissuersgen.ListOrganizationRemoteSessionsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// UpdateClient patches a client's non-secret fields in the caller's
// organization.
func (s *Service) UpdateClient(ctx context.Context, payload *orgissuersgen.UpdateClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	userSessionIssuerID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
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

	txRepo := repo.New(dbtx)

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

	beforeView, err := mv.BuildRemoteSessionClientView(existing)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}

	updated, err := txRepo.UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted:   clientSecretEncrypted,
		UserSessionIssuerID:     userSessionIssuerID,
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		ID:                      clientID,
		OrganizationID:          conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update organization admin remote session client").LogError(ctx, logger)
	}

	afterView, err := mv.BuildRemoteSessionClientView(updated)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(updated.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
		ClientID:               updated.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session client update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

// DeleteClient soft-deletes a client in the caller's organization and
// cascades the sessions minted against it.
func (s *Service) DeleteClient(ctx context.Context, payload *orgissuersgen.DeleteClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
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

	deleted, err := txRepo.DeleteOrganizationRemoteSessionClient(ctx, repo.DeleteOrganizationRemoteSessionClientParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete organization admin remote session client").LogError(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete dependent remote sessions").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDelete(ctx, dbtx, audit.LogRemoteSessionClientDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(deleted.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(deleted.ID),
		ClientID:               deleted.ClientID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization admin remote session client deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// RemoveClientFromMcpServer detaches a client from an MCP server by clearing
// the MCP server's user_session_issuer link, scoped to the caller's organization.
func (s *Service) RemoveClientFromMcpServer(ctx context.Context, payload *orgissuersgen.RemoveClientFromMcpServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}
	mcpServerID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp_server id").LogError(ctx, logger)
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

	// Establish org ownership of the client before detaching the MCP server.
	client, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	// Resolve the MCP server to find the user_session_issuer it uses (the binding
	// to remove) and its name (for the audit event). Scoped to the caller's org
	// (via the server's project) so a cross-org id resolves to NotFound rather
	// than reading a foreign-tenant row.
	server, err := mcpserversrepo.New(dbtx).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{
		ID:             mcpServerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}
	if !server.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "mcp server is not attached to this client").LogError(ctx, logger)
	}

	affected, err := txRepo.DetachRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachRemoteSessionClientFromUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   server.UserSessionIssuerID.UUID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach remote session client from user session issuer").LogError(ctx, logger)
	}
	if affected == 0 {
		return oops.E(oops.CodeNotFound, nil, "mcp server is not attached to this client").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDetachMcpServer(ctx, dbtx, audit.LogRemoteSessionClientDetachMcpServerEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(client.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		McpServerURN:           urn.NewMcpServer(server.ID),
		McpServerName:          conv.FromPGTextOrEmpty[string](server.Name),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization remote session client detach").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// RevokeSession soft-deletes a single session in the caller's organization.
func (s *Service) RevokeSession(ctx context.Context, payload *orgissuersgen.RevokeSessionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	sessionID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session id").LogError(ctx, logger)
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

	revoked, err := txRepo.RevokeOrganizationRemoteSession(ctx, repo.RevokeOrganizationRemoteSessionParams{
		ID:             sessionID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "revoke organization admin remote session").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionDelete(ctx, dbtx, audit.LogRemoteSessionDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        orgProjectID(revoked.ClientProjectID),
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RemoteSessionURN: urn.NewRemoteSession(revoked.ID),
		SubjectURN:       revoked.SubjectUrn,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization admin remote session revoke").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// RevokeAllClientSessions soft-deletes all sessions minted against a client
// in the caller's organization, recording a single bulk audit event.
func (s *Service) RevokeAllClientSessions(ctx context.Context, payload *orgissuersgen.RevokeAllClientSessionsPayload) (*orgissuersgen.RevokeAllRemoteSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	client, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	revokedCount, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, client.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke all remote sessions").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientRevokeSessions(ctx, dbtx, audit.LogRemoteSessionClientRevokeSessionsEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(client.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		RevokedCount:           revokedCount,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session revoke-all").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &orgissuersgen.RevokeAllRemoteSessionsResult{RevokedCount: int(revokedCount)}, nil
}

// orgProjectID flattens a nullable project id for audit events; org-level
// rows (NULL project_id) audit with uuid.Nil.
func orgProjectID(id uuid.NullUUID) uuid.UUID {
	if id.Valid {
		return id.UUID
	}
	return uuid.Nil
}
