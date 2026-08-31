package remotesessions

import (
	"context"
	"errors"
	"log/slog"
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

	// Revocation endpoint must be HTTPS, or HTTP on loopback where a token
	// never crosses a network: tokens are sensitive credentials that must not
	// be transmitted in plaintext. An empty value stays legal.
	if v := conv.PtrValOr(payload.RevocationEndpoint, ""); v != "" && !urls.IsAbsoluteHTTPSOrLoopback(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "revocation_endpoint must be an absolute https URL, or http on loopback").LogError(ctx, logger)
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
		RevocationEndpoint:                conv.PtrToPGText(payload.RevocationEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGTextEmpty(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGTextEmpty(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGTextEmpty(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		CodeChallengeMethodsSupported:     payload.CodeChallengeMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrValOr(payload.ClientIDMetadataDocumentSupported, false),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
		// Create does not discover, so it has no document to snapshot. The
		// column is filled by the first refresh, and by callers that already
		// hold a discovery document (see platformmcp's attachment).
		Metadata: nil,
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
		IncludeGlobal:  true,
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

	// Read-only, so platform issuers resolve here: the org listing surfaces them
	// and the detail view has to be able to open one.
	issuer, err := repo.New(s.db).GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  true,
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

	// Platform issuers are deliberately excluded. A tenant cannot delete one, so
	// the preflight is unreachable for them by design — and both queries below
	// are unscoped by organization, so resolving a platform issuer here would
	// report other tenants' client counts and MCP server names to this caller.
	// Keep IncludeGlobal false unless those queries are org-scoped first.
	if _, err := r.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  false,
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

// GetIssuerDuplicatePreflight reports the issuers an organization administrator
// can already see that describe a given upstream authorization server, so a
// create or edit form can warn before adding a second record for one issuer.
//
// The organization arm is the wide one — every record in the organization,
// project-specific ones included — rather than the organization-level-only arm
// the project tier uses. An org administrator holds org:read across the whole
// organization, and the project-specific rows are the most useful thing this can
// report: an administrator adding an organization-level issuer most needs to
// know that several projects already configured the same URL separately, since
// those are exactly what MigrateIssuer consolidates.
//
// The answer does not depend on whether the issuer being created will be
// organization-level or project-scoped. Both are written by CreateIssuer under
// the same org:admin grant, and narrowing the project-scoped case would hide
// duplicates the same caller can see a moment later in the issuer listing.
func (s *Service) GetIssuerDuplicatePreflight(ctx context.Context, payload *orgissuersgen.GetIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	canonical, err := parseCanonicalIssuerURL(conv.PtrValOrEmpty(payload.Issuer, ""))
	if err != nil {
		return emptyIssuerDuplicatePreflight(), nil
	}

	candidates, err := repo.New(s.db).ListOrganizationRemoteSessionIssuersByIssuerURL(ctx, repo.ListOrganizationRemoteSessionIssuersByIssuerURLParams{
		Issuers:        canonical.matchCandidates(),
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  true,
		PerTierLimit:   maxIssuerDuplicateMatchesPerTier,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization remote session issuers by issuer url").LogError(ctx, logger)
	}

	rows := make([]issuerDuplicateCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, issuerDuplicateCandidate{
			id:          candidate.ID,
			slug:        candidate.Slug,
			name:        conv.FromPGTextOrEmpty[string](candidate.Name),
			issuerURL:   candidate.Issuer,
			tier:        scopeOfTenancy(candidate.ProjectID, candidate.OrganizationID),
			projectName: candidate.ProjectName,
		})
	}

	return buildIssuerDuplicatePreflight(rows), nil
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

	// An empty logo asset id stays legal: the update query reads it as the
	// explicit "clear to NULL" sentinel. Any other value must be a uuid —
	// the query casts the text parameter, so a malformed value has to be
	// rejected here rather than surfacing as a Postgres cast error.
	if v := conv.PtrValOr(payload.LogoAssetID, ""); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
		}
	}

	// Revocation endpoint must be HTTPS, or HTTP on loopback where a token
	// never crosses a network: tokens are sensitive credentials that must not
	// be transmitted in plaintext. An empty value stays legal.
	if v := conv.PtrValOr(payload.RevocationEndpoint, ""); v != "" && !urls.IsAbsoluteHTTPSOrLoopback(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "revocation_endpoint must be an absolute https URL, or http on loopback").LogError(ctx, logger)
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

	// A tenant must never edit a platform issuer: it is shared across every
	// organization and curated by platform admins.
	// UpdateOrganizationRemoteSessionIssuer below is org-scoped and would refuse
	// anyway; opting the pre-read out keeps the refusal a clean 404.
	existing, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  false,
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
		LogoAssetID:                       conv.PtrToPGText(payload.LogoAssetID),
		ClientSetupDocumentationUrl:       conv.PtrToPGText(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RevocationEndpoint:                conv.PtrToPGText(payload.RevocationEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGText(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGText(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGText(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		CodeChallengeMethodsSupported:     payload.CodeChallengeMethodsSupported,
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

// FetchIssuerMetadata fetches an upstream issuer's RFC 8414 metadata document
// and returns a draft suitable for CreateIssuer. Keyed by issuer URL, so no
// record need exist and nothing is persisted.
//
// The organization-scoped counterpart of
// remoteSessionIssuers.fetchMetadata. Creating an organization-level issuer
// through the project-scoped one meant authorizing against whichever project
// happened to be active in the operator's session, for a resource that has no
// project at all.
func (s *Service) FetchIssuerMetadata(ctx context.Context, payload *orgissuersgen.FetchIssuerMetadataPayload) (*types.RemoteSessionIssuerDraft, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	// Unlike the project tier, which leaves its stateless fetch ungated because
	// there is no project resource to gate, this one is gated: every other
	// method on this service requires org:admin, and the surface it feeds — the
	// org-admin create sheet — is behind that scope already. Gating costs
	// nothing here and keeps the service's authorization uniform.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	issuerURL := strings.TrimSpace(payload.Issuer)
	if issuerURL == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	if !urls.IsAbsoluteHTTP(issuerURL) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid issuer url").LogError(ctx, logger)
	}

	doc, _, warnings, err := discoverIssuerMetadata(ctx, s.policy, issuerURL)
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, err, oops.CodeBadRequest)
	}

	return buildIssuerDraft(doc, issuerURL, warnings), nil
}

// RefreshIssuerMetadata re-reads an existing issuer's RFC 8414 metadata
// document and persists the discovered values, returning the updated issuer
// alongside any warnings.
//
// One endpoint serves both tables on the Remote Identity Providers page:
// GetOrganizationRemoteSessionIssuerByID matches organizational and
// project-specific rows alike, and org:admin covers both.
func (s *Service) RefreshIssuerMetadata(ctx context.Context, payload *orgissuersgen.RefreshIssuerMetadataPayload) (*types.RemoteSessionIssuerRefresh, error) {
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

	// Read outside the transaction: discovery below is an upstream HTTP call
	// under a ten-second budget and must not hold a pooled connection. The
	// update re-asserts this row's identity, so a concurrent move or issuer
	// rename aborts the write rather than letting the gap be exploited.
	//
	// IncludeGlobal is false because this persists discovered metadata: a tenant
	// must never rewrite a platform issuer's endpoints. The row-locked re-read
	// below is org-scoped and would refuse anyway; opting the pre-read out keeps
	// the refusal a clean 404.
	existing, err := repo.New(s.db).GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  false,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	params, warnings, err := refreshIssuerMetadata(ctx, s.policy, existing)
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, err, oops.CodeGatewayError)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Re-read under a row lock rather than reusing the pre-discovery read: an
	// updateIssuer that committed while discovery ran would otherwise land in
	// this entry's before/after diff and be attributed to the refresh. The lock
	// is taken after discovery finished, so it is never held across the upstream
	// call.
	locked, err := txRepo.GetOrganizationRemoteSessionIssuerByIDForUpdate(ctx, repo.GetOrganizationRemoteSessionIssuerByIDForUpdateParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "%s", refreshConflictMessage).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock organization admin remote session issuer").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(locked)

	updated, err := txRepo.UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "%s", refreshConflictMessage).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update organization admin remote session issuer discovered metadata").LogError(ctx, logger)
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

	return &types.RemoteSessionIssuerRefresh{Issuer: afterView, DiscoveryWarnings: warnings}, nil
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

	// Serialize the count-then-delete below against client creation: every
	// client writer takes this advisory lock before binding a client to the
	// issuer. Without it a create commits in the gap and strands a live client
	// on a deleted issuer, because the soft delete only rewrites deleted_at and
	// its FOR NO KEY UPDATE row lock does not conflict with the FOR KEY SHARE
	// the client insert's foreign key takes. Taking the advisory lock before any
	// row lock also matches the order the create paths use, so neither can
	// deadlock against the other.
	if err := txRepo.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}

	// Establish org ownership of the issuer before counting clients or deleting,
	// so a cross-org id returns NotFound rather than probing client counts or
	// silently succeeding against the org-scoped delete below. Platform issuers
	// are excluded for the same reason: a tenant must never delete one, and
	// CountRemoteSessionClientsByIssuerID below is unscoped by organization.
	if _, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  false,
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

	// A platform issuer has no owning organization to re-scope within, and a
	// tenant must never move one. SetOrganizationRemoteSessionIssuerProject is
	// org-scoped and would refuse anyway; opting out keeps it a clean 404.
	existing, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  false,
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

// loadMigrationPair resolves the source and target issuers, both scoped to the
// caller's organization, and validates the scope ladder between them. It is the
// shared entry check for getIssuerMigratePreflight and migrateIssuer so the
// dialog and the mutation agree on which pairs are addressable at all.
//
// forUpdate row-locks both issuers for the rest of the transaction. The mutation
// passes true so that the scope and endpoint metadata it validates cannot be
// rewritten by a concurrent moveIssuer or updateIssuer before it acts on them;
// the read-only preflight passes false. Callers that lock must already hold the
// advisory locks from lockIssuersForMigration, which order the row locks and so
// keep two concurrent migrations of the same pair from deadlocking.
func loadMigrationPair(ctx context.Context, r *repo.Queries, logger *slog.Logger, organizationID, sourceIDRaw, targetIDRaw string, forUpdate bool) (source, target repo.RemoteSessionIssuer, err error) {
	sourceID, err := uuid.Parse(sourceIDRaw)
	if err != nil {
		return source, target, oops.E(oops.CodeBadRequest, err, "invalid source issuer id").LogError(ctx, logger)
	}
	targetID, err := uuid.Parse(targetIDRaw)
	if err != nil {
		return source, target, oops.E(oops.CodeBadRequest, err, "invalid target issuer id").LogError(ctx, logger)
	}

	if sourceID == targetID {
		return source, target, oops.E(oops.CodeBadRequest, nil, "source and target issuer must differ").LogError(ctx, logger)
	}

	// Both arms stay org-scoped, permanently. Migrating onto a platform issuer is
	// a platform-admin operation with its own loader, loadPlatformMigrationPair,
	// which reads the source from the tenant partition and the target from the
	// global one. Widening either arm here would not reproduce that: there is
	// deliberately no global-inclusive ForUpdate variant, so a widened non-locking
	// arm alone would let a migration validate against a scope it never locked.
	loadIssuer := func(id uuid.UUID) (repo.RemoteSessionIssuer, error) {
		if forUpdate {
			return r.GetOrganizationRemoteSessionIssuerByIDForUpdate(ctx, repo.GetOrganizationRemoteSessionIssuerByIDForUpdateParams{
				ID:             id,
				OrganizationID: conv.ToPGText(organizationID),
			})
		}
		return r.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
			ID:             id,
			OrganizationID: conv.ToPGText(organizationID),
			IncludeGlobal:  false,
		})
	}

	source, err = loadIssuer(sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return source, target, oops.E(oops.CodeNotFound, err, "source remote session issuer not found").LogError(ctx, logger)
		}
		return source, target, oops.E(oops.CodeUnexpected, err, "get source remote session issuer").LogError(ctx, logger)
	}

	target, err = loadIssuer(targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return source, target, oops.E(oops.CodeNotFound, err, "target remote session issuer not found").LogError(ctx, logger)
		}
		return source, target, oops.E(oops.CodeUnexpected, err, "get target remote session issuer").LogError(ctx, logger)
	}

	var scopeErr migrationScopeError
	if err := validateMigrationScope(source, target); errors.As(err, &scopeErr) {
		return source, target, oops.E(oops.CodeBadRequest, err, "%s", scopeErr.reason).LogError(ctx, logger)
	} else if err != nil {
		return source, target, oops.E(oops.CodeUnexpected, err, "validate migration scope").LogError(ctx, logger)
	}

	return source, target, nil
}

// GetIssuerMigratePreflight reports what consolidating the source issuer onto
// the target would do, and every blocker that would make it fail, so the
// confirmation dialog is authoritative before the mutation runs.
func (s *Service) GetIssuerMigratePreflight(ctx context.Context, payload *orgissuersgen.GetIssuerMigratePreflightPayload) (*orgissuersgen.OrganizationIssuerMigratePreflight, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	source, target, err := loadMigrationPair(ctx, r, logger, authCtx.ActiveOrganizationID, payload.SourceID, payload.TargetID, false)
	if err != nil {
		return nil, err
	}

	preflight, err := buildMigratePreflight(ctx, r, source, target)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session issuer migrate preflight").LogError(ctx, logger)
	}

	return &orgissuersgen.OrganizationIssuerMigratePreflight{
		ClientCount:               int(preflight.clientCount),
		McpServerNames:            preflight.mcpServerNames,
		EndpointMismatches:        issuerFieldMismatchViews(preflight.endpointMismatches),
		ConflictingMcpServerNames: preflight.conflictingMcpServerNames,
		Warnings:                  issuerFieldMismatchViews(preflight.warnings),
		CanMigrate:                preflight.canMigrate(),
	}, nil
}

// MigrateIssuer consolidates the source issuer onto the target: every active
// client is re-pointed onto the target and the now-empty source is soft-deleted,
// in one transaction. Remote sessions reference the client rather than the
// issuer, so they survive the re-point untouched and no user re-authenticates.
//
// Re-pointing strictly precedes the soft-delete because the runtime resolution
// query filters `i.deleted IS FALSE`: a client left on a tombstoned issuer stops
// resolving. Holding both in one transaction removes the window entirely.
func (s *Service) MigrateIssuer(ctx context.Context, payload *orgissuersgen.MigrateIssuerPayload) (*orgissuersgen.MigrateOrganizationRemoteSessionIssuerResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Establish that both issuers exist in the caller's organization before
	// taking any lock, so a caller can never advisory-lock an issuer id that
	// belongs to another organization.
	source, target, err := loadMigrationPair(ctx, txRepo, logger, authCtx.ActiveOrganizationID, payload.SourceID, payload.TargetID, false)
	if err != nil {
		return nil, err
	}

	// Serialize against a concurrent client attach on either issuer before
	// reading the conflict set, so the set we act on cannot go stale under us.
	// Nothing in the schema enforces the one-client-per-(user_session_issuer,
	// remote_session_issuer) invariant, so this advisory lock is the only thing
	// standing between a racing attach and a duplicate binding.
	if err := lockIssuersForMigration(ctx, txRepo, source.ID, target.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock issuers for migration").LogError(ctx, logger)
	}

	// Re-read both issuers under a row lock and re-validate the scope ladder.
	// The advisory lock above only serializes writers that take it, and neither
	// moveIssuer (which rewrites project_id) nor updateIssuer (which rewrites the
	// endpoints) does. Without this the scope and parity guards below would run
	// against rows a concurrent transaction could still change before we commit.
	source, target, err = loadMigrationPair(ctx, txRepo, logger, authCtx.ActiveOrganizationID, payload.SourceID, payload.TargetID, true)
	if err != nil {
		return nil, err
	}

	clientsMigrated, err := runIssuerMigration(ctx, txRepo, logger, source, target)
	if err != nil {
		return nil, err
	}

	// The source now has no active clients, so the delete guard that
	// DeleteIssuer applies is satisfied by construction.
	deleted, err := txRepo.DeleteOrganizationRemoteSessionIssuer(ctx, repo.DeleteOrganizationRemoteSessionIssuerParams{
		ID:             source.ID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "soft-delete migrated remote session issuer").LogError(ctx, logger)
	}

	targetView := mv.BuildRemoteSessionIssuerView(target)

	if err := s.auditLogger.LogRemoteSessionIssuerMigrate(ctx, dbtx, audit.LogRemoteSessionIssuerMigrateEvent{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      orgProjectID(deleted.ProjectID),

		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,

		SourceRemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(deleted.ID),
		SourceSlug:                   deleted.Slug,
		SourceIssuerURL:              deleted.Issuer,
		SourceName:                   conv.FromPGText[string](deleted.Name),

		TargetRemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(target.ID),
		TargetSlug:                   target.Slug,

		ClientsMigrated: clientsMigrated,

		SnapshotBefore: mv.BuildRemoteSessionIssuerView(source),
		SnapshotAfter:  targetView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session issuer migration").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &orgissuersgen.MigrateOrganizationRemoteSessionIssuerResult{
		Issuer:          targetView,
		ClientsMigrated: int(clientsMigrated),
		SourceDeleted:   true,
	}, nil
}

// orgProjectID flattens a nullable project id for audit events; org-level
// rows (NULL project_id) audit with uuid.Nil.
func orgProjectID(id uuid.NullUUID) uuid.UUID {
	if id.Valid {
		return id.UUID
	}
	return uuid.Nil
}
