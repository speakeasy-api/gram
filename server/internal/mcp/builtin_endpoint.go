package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	builtinrepo "github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func (e *ResolvedMcpEndpoint) IsBuiltin() bool { return e.RouteBase == mcproute.Builtin }

func builtinRequestSlug(r *http.Request) string {
	return chi.URLParam(r, "organizationSlug") + "/" + chi.URLParam(r, "projectSlug") + "/" + chi.URLParam(r, "builtinKey")
}

func validBuiltinSlug(slug string) bool {
	parts := strings.Split(slug, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return false
	}
	_, ok := builtinmcp.Lookup(parts[2])
	return ok
}

// loadBuiltinEndpoint resolves a code-owned resource, not a stored MCP server.
// All runtime and OAuth entry points use this same live ownership/issuer lookup.
func (s *Service) loadBuiltinEndpoint(ctx context.Context, slug string) (*ResolvedMcpEndpoint, error) {
	if !validBuiltinSlug(slug) {
		return nil, oops.C(oops.CodeNotFound)
	}
	parts := strings.Split(slug, "/")
	org, err := s.orgsRepo.GetOrganizationMetadataBySlug(ctx, parts[0])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve built-in organization: %w", err)
	}
	if org.DisabledAt.Valid {
		return nil, oops.C(oops.CodeNotFound)
	}
	project, err := projectsrepo.New(s.db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{OrganizationID: org.ID, Slug: parts[1]})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve built-in project: %w", err)
	}
	var domainID uuid.NullUUID
	if domain := customdomains.FromContext(ctx); domain != nil {
		if domain.OrganizationID != org.ID {
			return nil, oops.C(oops.CodeNotFound)
		}
		domainID = uuid.NullUUID{UUID: domain.DomainID, Valid: true}
	}
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		started := time.Now()
		authority, err := networkingress.LoadRequestAuthority(ctx, s.db)
		result, reason := networkingress.ResultAllowed, networkingress.ReasonNone
		if err != nil || authority.OrganizationID != org.ID {
			result, reason = networkingress.ResultDenied, networkingress.ReasonAuthorityRejected
			if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
				result, reason = networkingress.ResultError, networkingress.ReasonAuthorityUnavailable
			}
		}
		s.networkIngressTelemetry.Record(ctx, networkingress.OperationResolution, result, reason, "builtin", time.Since(started))
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			return nil, oops.E(oops.CodeUnavailable, err, "private network ingress authority is unavailable")
		}
		if err != nil || authority.OrganizationID != org.ID {
			return nil, oops.C(oops.CodeNotFound)
		}
		if _, err := privateEndpointNamespace(authority.NamespaceKind); err != nil {
			return nil, oops.C(oops.CodeNotFound)
		}
		domainID = authority.CustomDomainID
	}
	if err := s.enforceAgentGatewayCustomDomainLockdown(ctx, s.logger, org.ID); err != nil {
		return nil, err
	}
	issuerID, issuerErr := s.builtinIssuer(ctx, org.ID, project.ID, parts[2])
	endpoint := &ResolvedMcpEndpoint{ //nolint:exhaustruct // synthetic: no persisted backend references
		BuiltinID:      uuid.NullUUID{UUID: builtinmcp.ID(project.ID, parts[2]), Valid: true},
		OrganizationID: org.ID, ProjectID: project.ID, Slug: slug, RouteBase: mcproute.Builtin,
		CustomDomainID: domainID, UserSessionIssuerID: issuerID, builtinOAuthError: issuerErr,
	}
	if issuerID != uuid.Nil {
		endpoint.AudienceURN = urn.NewUserSessionIssuer(issuerID).String()
		if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
			endpoint.builtinOAuthError = err
			endpoint.UserSessionIssuerID = uuid.Nil
			endpoint.AudienceURN = ""
		}
	}
	return endpoint, nil
}

// Prefer a project authority to an organization authority. Ambiguous tiers
// fail closed: picking an arbitrary issuer would silently change trust.
func (s *Service) builtinIssuer(ctx context.Context, organizationID string, projectID uuid.UUID, key string) (uuid.UUID, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin built-in issuer selection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := builtinmcp.LoadSettings(ctx, tx, projectID, key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read built-in issuer binding: %w", err)
	}
	if before.UserSessionIssuerBound {
		if !before.UserSessionIssuerID.Valid {
			return uuid.Nil, oops.E(oops.CodeUnavailable, nil, "bound built-in OAuth issuer is unavailable")
		}
		if _, err := builtinrepo.New(tx).ValidateUserSessionIssuer(ctx, builtinrepo.ValidateUserSessionIssuerParams{
			ID: before.UserSessionIssuerID.UUID, ProjectID: projectID, OrganizationID: organizationID,
		}); err != nil {
			return uuid.Nil, oops.E(oops.CodeUnavailable, err, "bound built-in OAuth issuer is unavailable")
		}
		return before.UserSessionIssuerID.UUID, nil
	}
	var cursor uuid.NullUUID
	var projectIssuers, organizationIssuers []uuid.UUID
	for {
		rows, err := usersessionsrepo.New(tx).ListUserSessionIssuersByProjectID(ctx, usersessionsrepo.ListUserSessionIssuersByProjectIDParams{ProjectID: projectID, OrganizationID: organizationID, Cursor: cursor, LimitValue: 100})
		if err != nil {
			return uuid.Nil, fmt.Errorf("resolve built-in issuer: %w", err)
		}
		for _, issuer := range rows {
			if issuer.ProjectID.Valid {
				projectIssuers = append(projectIssuers, issuer.ID)
			} else {
				organizationIssuers = append(organizationIssuers, issuer.ID)
			}
		}
		if len(rows) < 100 {
			break
		}
		cursor = uuid.NullUUID{UUID: rows[len(rows)-1].ID, Valid: true}
	}
	candidates := projectIssuers
	if len(candidates) == 0 {
		candidates = organizationIssuers
	}
	if len(candidates) > 1 {
		return uuid.Nil, oops.E(oops.CodeUnavailable, nil, "built-in MCP issuer is ambiguous")
	}
	if len(candidates) == 0 {
		return uuid.Nil, nil
	}
	// Hold the live issuer row through persistence, serializing first binding
	// against issuer deletion just like an explicit administrator update.
	if _, err := builtinrepo.New(tx).ValidateUserSessionIssuer(ctx, builtinrepo.ValidateUserSessionIssuerParams{
		ID: candidates[0], ProjectID: projectID, OrganizationID: organizationID,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("validate built-in issuer: %w", err)
	}
	// The compare-and-set never overwrites an explicit administrator choice or
	// another successful first selection. Unauthenticated callers cannot supply
	// an issuer ID; they can only materialize the unique live default.
	_, err = builtinrepo.New(tx).BindUserSessionIssuer(ctx, builtinrepo.BindUserSessionIssuerParams{
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, BuiltinKey: key,
		UserSessionIssuerID: uuid.NullUUID{UUID: candidates[0], Valid: true},
	})
	selected := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("bind built-in issuer: %w", err)
	}
	after, err := builtinmcp.LoadSettings(ctx, tx, projectID, key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read committed built-in issuer binding: %w", err)
	}
	if selected {
		if err := s.auditLogger.LogBuiltinMCPUpdate(ctx, tx, organizationID, projectID, builtinmcp.ID(projectID, key), key, urn.NewSystemPrincipal("builtin-mcp-issuer-binding"), before, after); err != nil {
			return uuid.Nil, fmt.Errorf("audit built-in issuer binding: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit built-in issuer binding: %w", err)
	}
	if !after.UserSessionIssuerBound || !after.UserSessionIssuerID.Valid {
		return uuid.Nil, oops.E(oops.CodeUnavailable, nil, "bound built-in OAuth issuer is unavailable")
	}
	return after.UserSessionIssuerID.UUID, nil
}

func (s *Service) loadBuiltinOAuthEndpoint(ctx context.Context, slug string) (*ResolvedMcpEndpoint, error) {
	endpoint, err := s.loadBuiltinEndpoint(ctx, slug)
	if err != nil {
		return nil, err
	}
	if endpoint.builtinOAuthError != nil {
		return nil, endpoint.builtinOAuthError
	}
	if endpoint.UserSessionIssuerID == uuid.Nil {
		return nil, oops.C(oops.CodeNotFound)
	}
	return endpoint, nil
}

func (s *Service) withBuiltinEndpoint(serve func(http.ResponseWriter, *http.Request, *ResolvedMcpEndpoint) error) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		endpoint, err := s.loadBuiltinOAuthEndpoint(r.Context(), builtinRequestSlug(r))
		if err != nil {
			return err
		}
		return serve(w, r, endpoint)
	}
}

func (s *Service) builtinOAuthRoutes() []organizationGatewayOAuthRoute {
	return []organizationGatewayOAuthRoute{
		{http.MethodGet, wellknown.OAuthProtectedResourcePath + BuiltinRoute, s.ServeGetProtectedResource, false},
		{http.MethodGet, wellknown.OAuthAuthorizationServerPath + BuiltinRoute, s.ServeGetAuthorizationServer, true},
		{http.MethodPost, BuiltinRoute + "/register", s.ServeRegister, true},
		{http.MethodGet, BuiltinRoute + "/authorize", s.ServeAuthorize, true},
		{http.MethodGet, BuiltinRoute + "/connect", s.ServeConsent, true},
		{http.MethodPost, BuiltinRoute + "/connect", s.ServeConsent, true},
		{http.MethodPost, BuiltinRoute + "/connect/remote-session", s.ServeConsentAction, true},
		{http.MethodGet, BuiltinRoute + "/connect/first-party", s.ServeFirstPartyConnect, true},
		{http.MethodPost, BuiltinRoute + "/token", s.ServeToken, true},
		{http.MethodPost, BuiltinRoute + "/revoke", s.ServeRevoke, true},
	}
}

func attachBuiltinOAuth(mux goahttp.Muxer, service *Service) {
	for _, route := range service.builtinOAuthRoutes() {
		o11y.AttachHandler(mux, route.method, route.path, oops.ErrHandle(service.logger, service.withBuiltinEndpoint(route.serve)).ServeHTTP)
	}
}

func attachBuiltinAuthenticationHost(host *AuthenticationHost, service *Service) {
	for _, route := range service.builtinOAuthRoutes() {
		if route.onAuthenticationHost {
			host.Handle(route.method, route.path, oops.ErrHandle(service.logger, service.withBuiltinEndpoint(route.serve)).ServeHTTP)
		}
	}
}

// checkEndpointAgentConnections checks only credentials used by this backend.
// Built-in tools use the declared environment, not issuer-linked remote clients.
func (s *Service) checkEndpointAgentConnections(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject) error {
	if endpoint.IsBuiltin() {
		return nil
	}
	if err := s.remoteChallengeMgr.CheckAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject); err != nil {
		return fmt.Errorf("check endpoint agent connections: %w", err)
	}
	return nil
}

// Cached callbacks resume in the resource's recorded namespace, not the global
// callback host's namespace. Validate live authority before reconstructing it;
// normal continuation handlers still validate the actual incoming request.
func (s *Service) loadBuiltinEndpointByRef(ctx context.Context, ref EndpointRef) (*ResolvedMcpEndpoint, error) {
	if err := ref.Authority.ValidateEndpointRef(ref.BaseURL, ref.CustomDomainID); err != nil {
		return nil, errToolsetEndpointMismatch
	}
	if err := ref.Authority.ValidateLive(ctx, s.db); err != nil {
		return nil, fmt.Errorf("validate built-in OAuth authority: %w", err)
	}
	resourceCtx := customdomains.WithContext(ctx, nil)
	if ref.Authority.Surface != "" {
		resourceCtx = requestorigin.WithContext(resourceCtx, requestorigin.Origin{Surface: ref.Authority.Surface, BaseURL: ref.BaseURL, OrganizationID: ref.Authority.OrganizationID, NetworkIngressID: ref.Authority.NetworkIngressID, NetworkIdentity: nil})
	}
	if ref.CustomDomainID.Valid {
		domain, err := customdomainsrepo.New(s.db).GetCustomDomainByID(ctx, ref.CustomDomainID.UUID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("load built-in OAuth domain: %w", err)
		}
		if !domain.Activated || !domain.Verified {
			return nil, oops.C(oops.CodeNotFound)
		}
		resourceCtx = customdomains.WithContext(resourceCtx, &customdomains.Context{OrganizationID: domain.OrganizationID, DomainID: domain.ID, Domain: domain.Domain})
	}
	endpoint, err := s.loadBuiltinOAuthEndpoint(resourceCtx, ref.McpSlug)
	if err != nil {
		return nil, err
	}
	// The shared loader restores shared-AS context after this builder returns;
	// full ValidateRef runs at the continuation boundary with that context set.
	if endpoint.BuiltinID != ref.BuiltinID || endpoint.CustomDomainID != ref.CustomDomainID ||
		(ref.Authority.Surface != "" && endpoint.OrganizationID != ref.Authority.OrganizationID) {
		return nil, errToolsetEndpointMismatch
	}
	return endpoint, nil
}
