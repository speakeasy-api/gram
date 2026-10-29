// The organization gateway MCP surface: one URL per organization, signed in
// to through the organization's own user session issuer, whose members are
// every server in the organization the signed-in user can reach. Like the
// agent gateway it has no stored row and reuses the meta-server runtime
// wholesale; what is particular to it is how the organization and its issuer
// are found and that upstream credentials resolve per member project.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// OrganizationGatewayRoute is the organization gateway's path, addressed by
// organization slug. The fixed "gateway" segment keeps organization slugs out
// of the endpoint slug namespace: chi matches it before /mcp/{mcpSlug}/....
// A new platform endpoint cannot take the bare slug "gateway", because
// platform endpoint slugs carry their organization's prefix, but one on a
// custom domain can; shadowedEndpointHandler keeps its routes working.
//
// The parameter is named mcpSlug, as on every other MCP route, because the
// organization slug is this gateway's slug: the OAuth handlers it shares
// with stored endpoints read it under that name.
const OrganizationGatewayRoute = "/" + string(mcproute.OrganizationGateway) + "/{mcpSlug}"

// ProjectGatewayRoute restricts the derived gateway to one project.
const ProjectGatewayRoute = OrganizationGatewayRoute + "/projects/{projectSlug}"

// organizationGatewayNamespace derives the synthetic meta-server id an
// organization gateway reports. There is no meta_mcp_servers row behind it,
// so it must never be used to read one back; it exists so telemetry, session
// records, and cached challenges group per organization.
var organizationGatewayNamespace = uuid.MustParse("a6e17a1e-0000-4000-8000-000000000002")

func organizationGatewayID(organizationID string) uuid.UUID {
	return uuid.NewSHA1(organizationGatewayNamespace, []byte(organizationID))
}

// IsOrganizationGateway reports whether the endpoint is a derived organization
// or project gateway rather than a stored server or gateway.
func (e *ResolvedMcpEndpoint) IsOrganizationGateway() bool {
	return e.RouteBase == mcproute.OrganizationGateway
}

// organizationGateway is a resolved organization gateway: the synthetic rows
// the meta runtime is shaped around, and the issuer-gate endpoint built from
// them.
type organizationGateway struct {
	metaServer *metamcprepo.MetaMcpServer
	endpoint   *mcpendpointsrepo.McpEndpoint
	resolved   *ResolvedMcpEndpoint
}

// ServeOrganizationGateway terminates MCP for organization and project gateways.
func (s *Service) ServeOrganizationGateway(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	organizationSlug := organizationGatewayRequestSlug(r)
	gateway, err := s.loadOrganizationGateway(ctx, organizationSlug)
	if err != nil {
		return err
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(gateway.metaServer.OrganizationID),
		attr.SlogMetaMcpServerID(gateway.metaServer.ID.String()),
	)

	membership := metaMembership{agentID: uuid.Nil, organization: true}
	if err := s.serveResolvedMetaMCPEndpoint(w, r, logger, gateway.endpoint, gateway.metaServer, membership, gateway.resolved); err != nil {
		return fmt.Errorf("serve organization gateway: %w", err)
	}
	return nil
}

// shadowedEndpointSlug is the endpoint slug the organization gateway's fixed
// route segment shadows.
const shadowedEndpointSlug = "gateway"

// shadowedEndpointHandler serves POST /mcp/gateway/{name} for one of an
// endpoint's own POST routes. It is the endpoint's own route when an endpoint
// whose slug is "gateway" resolves on this host, and otherwise the
// organization gateway of the organization named name. Each answers with its
// own error shape: OAuth errors for the endpoint's routes, JSON-RPC errors
// for the gateway.
func (s *Service) shadowedEndpointHandler(name string, serve func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	endpointRoute := oauthRouteHandler(s.logger, http.MethodPost, "/"+name, serve)
	gatewayRoute := oops.MCPErrHandle(s.logger, s.ServeOrganizationGateway)
	return func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		if rctx == nil {
			rctx = chi.NewRouteContext()
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		}
		if _, err := s.loadResolvedMcpEndpointBySlug(r.Context(), s.logger, shadowedEndpointSlug, mcproute.MCP, mcpmetrics.LegacyFallbackOrgGatewayProbe); err == nil {
			rctx.URLParams.Add("mcpSlug", shadowedEndpointSlug)
			endpointRoute.ServeHTTP(w, r)
			return
		}
		rctx.URLParams.Add("mcpSlug", name)
		gatewayRoute.ServeHTTP(w, r)
	}
}

// organizationGatewayOAuthRoute is one route of the organization gateway's
// OAuth surface.
type organizationGatewayOAuthRoute struct {
	method string
	path   string
	serve  func(http.ResponseWriter, *http.Request, *ResolvedMcpEndpoint) error

	// onAuthenticationHost is set for the authorization server routes, which
	// the authentication host also serves for an issuer that opts in to it.
	// Protected resource metadata stays with the resource.
	onAuthenticationHost bool
}

func (s *Service) organizationGatewayOAuthRoutes() []organizationGatewayOAuthRoute {
	routes := []organizationGatewayOAuthRoute{
		{http.MethodGet, wellknown.OAuthProtectedResourcePath + OrganizationGatewayRoute, s.ServeGetProtectedResource, false},
		{http.MethodGet, wellknown.OAuthAuthorizationServerPath + OrganizationGatewayRoute, s.ServeGetAuthorizationServer, true},
		{http.MethodPost, OrganizationGatewayRoute + "/register", s.ServeRegister, true},
		{http.MethodGet, OrganizationGatewayRoute + "/authorize", s.ServeAuthorize, true},
		{http.MethodGet, OrganizationGatewayRoute + "/connect", s.ServeConsent, true},
		{http.MethodPost, OrganizationGatewayRoute + "/connect", s.ServeConsent, true},
		{http.MethodPost, OrganizationGatewayRoute + "/connect/remote-session", s.ServeConsentAction, true},
		{http.MethodPost, OrganizationGatewayRoute + "/connect/mcp", s.ServeConsentMCP, true},
		{http.MethodDelete, OrganizationGatewayRoute + "/connect/mcp", s.ServeConsentMCP, true},
		{http.MethodGet, OrganizationGatewayRoute + "/connect/first-party", s.ServeFirstPartyConnect, true},
		{http.MethodPost, OrganizationGatewayRoute + "/token", s.ServeToken, true},
		{http.MethodPost, OrganizationGatewayRoute + "/revoke", s.ServeRevoke, true},
	}
	for _, route := range slices.Clone(routes) {
		route.path = strings.Replace(route.path, OrganizationGatewayRoute, ProjectGatewayRoute, 1)
		routes = append(routes, route)
	}
	return routes
}

// shadowedEndpointRoute is one of an endpoint's own POST routes one segment
// below its slug. For an endpoint whose slug is "gateway", chi would otherwise
// match the organization gateway's route first, with the route name in the
// slug position.
type shadowedEndpointRoute struct {
	name  string
	serve func(http.ResponseWriter, *http.Request) error
}

func (s *Service) shadowedEndpointRoutes() []shadowedEndpointRoute {
	return []shadowedEndpointRoute{
		{"register", s.HandleRegister},
		{"token", s.HandleToken},
		{"revoke", s.HandleRevoke},
		{"connect", s.HandleConsent},
	}
}

// attachOrganizationGateway mounts the organization gateway and the OAuth
// surface under it on the public listener. Each OAuth route resolves the
// gateway first and hands it to the handler stored endpoints use, so the two
// cannot drift apart in what they serve.
func attachOrganizationGateway(mux goahttp.Muxer, service *Service) {
	o11y.AttachHandler(mux, http.MethodPost, ProjectGatewayRoute, oops.MCPErrHandle(service.logger, service.ServeOrganizationGateway).ServeHTTP)
	o11y.AttachHandler(mux, http.MethodPost, OrganizationGatewayRoute, oops.MCPErrHandle(service.logger, service.ServeOrganizationGateway).ServeHTTP)
	for _, route := range service.shadowedEndpointRoutes() {
		o11y.AttachHandler(mux, http.MethodPost, "/"+mcproute.OrganizationGateway.String()+"/"+route.name, service.shadowedEndpointHandler(route.name, route.serve))
	}
	for _, route := range service.organizationGatewayOAuthRoutes() {
		o11y.AttachHandler(mux, route.method, route.path, oauthRouteHandler(service.logger, route.method, route.path, service.withOrganizationGateway(route.serve, isOAuthTokenRoute(route.method, route.path))).ServeHTTP)
	}
}

// attachOrganizationGatewayAuthenticationHost mounts the organization
// gateway's authorization server routes on the authentication host, which an
// organization issuer that opts in to it announces as the gateway's
// authorization server.
func attachOrganizationGatewayAuthenticationHost(host *AuthenticationHost, service *Service) {
	for _, route := range service.shadowedEndpointRoutes() {
		host.Handle(http.MethodPost, "/"+mcproute.OrganizationGateway.String()+"/"+route.name, service.shadowedEndpointHandler(route.name, route.serve))
	}
	for _, route := range service.organizationGatewayOAuthRoutes() {
		if route.onAuthenticationHost {
			host.Handle(route.method, route.path, oauthRouteHandler(service.logger, route.method, route.path, service.withOrganizationGateway(route.serve, isOAuthTokenRoute(route.method, route.path))).ServeHTTP)
		}
	}
}

// withOrganizationGateway adapts an endpoint handler to a route addressed by
// organization slug.
func (s *Service) withOrganizationGateway(serve func(http.ResponseWriter, *http.Request, *ResolvedMcpEndpoint) error, tokenEndpoint bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		gateway, err := s.loadOrganizationGateway(r.Context(), organizationGatewayRequestSlug(r))
		if err != nil {
			if tokenEndpoint {
				return tokenEndpointResolutionError(err)
			}
			return err
		}
		return serve(w, r, gateway.resolved)
	}
}

// organizationGatewayRequestSlug retains the project path in the endpoint identity,
// so cached OAuth references and all advertised URLs preserve its scope.
func organizationGatewayRequestSlug(r *http.Request) string {
	slug := chi.URLParam(r, "mcpSlug")
	if project := chi.URLParam(r, "projectSlug"); project != "" {
		slug += "/projects/" + project
	}
	return slug
}

// loadOrganizationGateway resolves an organization slug, optionally followed by
// /projects/{projectSlug}, to its derived gateway.
// Every way the gateway can be unavailable reads as not found: an unknown
// organization, a request on a custom domain, and an organization
// orggateway.Availability serves none, including one whose custom domain
// locks its servers to an allowlist. Answering anything else would tell an
// anonymous caller which organizations exist.
func (s *Service) loadOrganizationGateway(ctx context.Context, gatewaySlug string) (*organizationGateway, error) {
	organizationSlug, projectSlug, scoped := strings.Cut(gatewaySlug, "/projects/")
	if strings.Contains(organizationSlug, "/") || (scoped && (projectSlug == "" || strings.Contains(projectSlug, "/"))) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if organizationSlug == "" {
		return nil, oops.C(oops.CodeNotFound)
	}
	// Served on the platform host only. A custom domain belongs to one
	// organization, and naming an organization in the path there would let
	// one organization's domain front another's gateway.
	if customdomains.FromContext(ctx) != nil {
		return nil, oops.C(oops.CodeNotFound)
	}

	organization, err := s.orgsRepo.GetOrganizationMetadataBySlug(ctx, organizationSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load organization").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogOrganizationID(organization.ID))

	issuerID, unavailable, err := orggateway.Availability(ctx, s.db, organization)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organization gateway issuer").LogError(ctx, logger)
	}
	if unavailable != orggateway.UnavailabilityNone {
		logger.DebugContext(ctx, "organization gateway unavailable", attr.SlogReason(string(unavailable)))
		return nil, oops.C(oops.CodeNotFound)
	}

	// Synthetic, never persisted: only the fields the meta runtime reads are
	// meaningful; the rest are zeroed rather than invented.
	metaServer := &metamcprepo.MetaMcpServer{
		ID:                  organizationGatewayID(organization.ID),
		OrganizationID:      organization.ID,
		ProjectID:           uuid.Nil,
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Name:                organization.Name,
		// NULL serves Speakeasy's built-in gateway instructions.
		Instructions:      pgtype.Text{String: "", Valid: false},
		DiscoveryMode:     "progressive",
		Visibility:        visibility.Private,
		NetworkAccessMode: pgtype.Text{String: "", Valid: false},
		CreatedAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UpdatedAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		DeletedAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Deleted:           false,
	}
	endpoint := &mcpendpointsrepo.McpEndpoint{ //nolint:exhaustruct // synthetic: only the fields the meta runtime reads are meaningful
		ProjectID: uuid.Nil,
		Slug:      organization.Slug,
	}

	if scoped {
		project, err := projectsrepo.New(s.db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{Slug: projectSlug, OrganizationID: organization.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load gateway project").LogError(ctx, logger)
		}
		metaServer.ID = uuid.NewSHA1(organizationGatewayNamespace, []byte(organization.ID+"/projects/"+project.ID.String()))
		metaServer.ProjectID = project.ID
		metaServer.Name = project.Name
		endpoint.ProjectID = project.ID
		endpoint.Slug = gatewaySlug
	}
	resolved := NewResolvedMcpEndpointFromMetaMcpServer(endpoint, metaServer, organization.ID, mcproute.OrganizationGateway)
	if err := s.RequireUserSessionIssuer(ctx, resolved); err != nil {
		return nil, err
	}

	return &organizationGateway{metaServer: metaServer, endpoint: endpoint, resolved: resolved}, nil
}

// projectTokenCache resolves a derived gateway's upstream credentials one
// member project at a time, on first use, and keeps them for the rest of the
// request.
//
// Remote session clients are bound per project: two projects may each bind
// their own client for one authorization server to the same organization
// issuer, so one organization-wide token map could not say which client's
// token belongs to which member. Resolving per project gives each member
// exactly the clients its own project, its organization, and the platform
// bind, which is what the member would get on its own endpoint.
type projectTokenCache struct {
	resolve func(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]remotesessions.UpstreamToken, error)

	mu        sync.Mutex
	byProject map[uuid.UUID]map[uuid.UUID]remotesessions.UpstreamToken
}

func (c *projectTokenCache) get(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tokens, ok := c.byProject[projectID]; ok {
		return tokens, nil
	}
	tokens, err := c.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	c.byProject[projectID] = tokens
	return tokens, nil
}

// newOrganizationGatewayTokens resolves upstream credentials for the
// authenticated subject the way a member's own endpoint would: partially,
// so a member whose provider is not connected degrades on its own. A stored
// gateway uses it too, for its hosted members, so the issuer's clients
// resolve only when one needs them.
func (s *Service) newOrganizationGatewayTokens(endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject) *projectTokenCache {
	return &projectTokenCache{
		resolve: func(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, projectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, s.logger)
			}
			return tokens, nil
		},
		mu:        sync.Mutex{},
		byProject: map[uuid.UUID]map[uuid.UUID]remotesessions.UpstreamToken{},
	}
}

// memberTokens is the upstream token map a member's dispatch routes from. A
// failure to resolve one project's credentials degrades that project's
// members alone, as any other member failure does; the cache has already
// logged the cause.
func (g *metaGateContext) memberTokens(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if g.memberChoices != nil && member.backend == metaMemberBackendProxied && g.memberChoices.serves(member) {
		return g.memberChoices.tokens(ctx, member)
	}
	if g.projectTokens == nil {
		return g.tokens, nil
	}
	tokens, err := g.projectTokens.get(ctx, member.projectID)
	if err != nil {
		return nil, &metaMemberError{message: fmt.Sprintf("server %q could not load its connection credentials; retry shortly", member.slug)}
	}
	return tokens, nil
}

// listEndpointClients lists the remote session clients an endpoint's consent
// covers: its project's own, or, for an organization gateway, the clients
// behind the servers the challenge's subject can reach through it.
//
// An organization gateway's issuer may carry clients from every project in
// the organization, and one person reaches only some of them. Listing the
// rest would show a person connections, and the services behind them, for
// servers their access does not include.
//
// Where typed auth client resolution is enforced, the registrations its
// servers offer through typed auth clients are listed too, whether or not the
// sign-in links them.
func (s *Service) listEndpointClients(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) ([]remotesessions.Client, error) {
	clients, _, err := s.listEndpointClientsAndRegistrations(ctx, endpoint, challengeState)
	return clients, err
}

// listEndpointClientsAndRegistrations shares one lazy member snapshot between
// sign-in filtering and typed registration discovery. Disabled enforcement
// does not trigger member discovery just to return an empty registration map.
func (s *Service) listEndpointClientsAndRegistrations(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) ([]remotesessions.Client, map[uuid.UUID]uuid.UUID, error) {
	var members []metaMember
	var membersErr error
	loaded := false
	loadMembers := func() ([]metaMember, error) {
		if !loaded {
			members, membersErr = s.consentSubjectMembers(ctx, endpoint, challengeState)
			loaded = true
		}
		return members, membersErr
	}
	clients, err := s.listSignInClients(ctx, endpoint, challengeState, loadMembers)
	if err != nil {
		return nil, nil, err
	}
	typed, err := s.typedConsentRegistrations(ctx, endpoint, loadMembers)
	if err != nil {
		return nil, nil, err
	}
	clients, err = s.withTypedRegistrationClients(ctx, endpoint.OrganizationID, clients, typed)
	return clients, typed, err
}

// listSignInClients is the clients the endpoint's sign-in links that
// listEndpointClients offers.
func (s *Service) listSignInClients(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, loadMembers func() ([]metaMember, error)) ([]remotesessions.Client, error) {
	if endpoint.IsOrganizationGateway() {
		clients, err := s.remoteChallengeMgr.ListOrganizationClients(ctx, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
		if err != nil {
			return nil, fmt.Errorf("list organization remote session clients: %w", err)
		}
		if len(clients) == 0 {
			return nil, nil
		}
		members, err := loadMembers()
		if err != nil {
			return nil, err
		}
		reachable := make([]remotesessions.Client, 0, len(clients))
		for _, client := range clients {
			if clientServesAnyMember(client, members) {
				reachable = append(reachable, client)
			}
		}
		return reachable, nil
	}
	clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("list remote session clients: %w", err)
	}
	if len(clients) == 0 {
		return clients, nil
	}
	// The issuer may bind clients for other servers sharing it. Consent offers
	// a proxied server only the clients configured for it, and a gateway only
	// those of the members the subject reaches. A hosted server keeps serving
	// from every client its issuer binds.
	switch {
	case endpoint.McpServerID.Valid && !endpoint.ToolsetID.Valid:
		associated, err := s.serverAssociatedClients(ctx, endpoint.OrganizationID, []uuid.UUID{endpoint.McpServerID.UUID})
		if err != nil {
			return nil, err
		}
		own := associated[endpoint.McpServerID.UUID]
		return slices.DeleteFunc(clients, func(client remotesessions.Client) bool {
			return !own[client.ID] || (challengeState.WorkloadReconnect != nil && client.ID != challengeState.WorkloadReconnect.ClientID)
		}), nil
	case endpoint.MetaMcpServerID.Valid:
		members, err := loadMembers()
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(clients, func(client remotesessions.Client) bool { return !clientServesAnyMember(client, members) }), nil
	}
	return clients, nil
}

// consentSubjectMembers resolves the gateway members the challenge's subject
// is admitted to, as its runtime requests will be.
func (s *Service) consentSubjectMembers(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) ([]metaMember, error) {
	if challengeState.Subject == nil {
		return nil, nil
	}
	// A session id keeps RBAC enforced: a context without one reads as an
	// internal call.
	memberCtx, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
	if err != nil {
		return nil, fmt.Errorf("stamp consent subject context: %w", err)
	}
	memberCtx = withMandatoryAdmission(memberCtx)
	_, members, err := s.resolveEndpointMemberSnapshot(memberCtx, s.logger, mandatorySurfaceConsent, endpoint)
	if err != nil {
		return nil, fmt.Errorf("resolve gateway members: %w", err)
	}
	if err := s.withTypedMemberRegistrations(ctx, endpoint.OrganizationID, members); err != nil {
		return nil, err
	}
	return members, nil
}

// clientServesAnyMember reports whether a client authenticates for at least
// one member, by memberUsesClient.
func clientServesAnyMember(client remotesessions.Client, members []metaMember) bool {
	for _, member := range members {
		if memberUsesClient(member, client.ID, client.RemoteSessionIssuerID, client.ProjectID) {
			return true
		}
	}
	return false
}

// endpointRemoteSessionStatuses is the subject's remote session state on
// every client listEndpointClients returns.
func (s *Service) endpointRemoteSessionStatuses(ctx context.Context, subject urn.SessionSubject, endpoint *ResolvedMcpEndpoint) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
	if endpoint.IsOrganizationGateway() {
		statuses, err := s.remoteChallengeMgr.OrganizationRemoteSessionStatuses(ctx, subject, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
		if err != nil {
			return nil, fmt.Errorf("list organization remote session statuses: %w", err)
		}
		return statuses, nil
	}
	statuses, err := s.remoteChallengeMgr.RemoteSessionStatuses(ctx, subject, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("list remote session statuses: %w", err)
	}
	return statuses, nil
}

// clientProjectID is the project an operation on one of the endpoint's
// clients runs under. An organization gateway has no project of its own, so
// each client is addressed in the project that owns it; an organization-level
// client belongs to none and is reachable from uuid.Nil.
func (e *ResolvedMcpEndpoint) clientProjectID(client remotesessions.Client) uuid.UUID {
	if e.IsOrganizationGateway() {
		return client.ProjectID.UUID
	}
	return e.ProjectID
}

// clientTokenProjectIDs are the projects a client's upstream tokens may
// resolve under, in the order to try them. Tokens resolve within one
// project's bindings; on an organization gateway that is the client's own
// project, or, for an organization-level client that belongs to none, each
// project with a server that authenticates through it. A project that also
// binds a client of its own for the same authorization server routes that
// client instead, so callers must try each until one routes this client.
// A project gateway restricts both cases to its own project.
// None means no server on the gateway uses the client.
func (s *Service) clientTokenProjectIDs(ctx context.Context, endpoint *ResolvedMcpEndpoint, client remotesessions.Client) ([]uuid.UUID, error) {
	if !endpoint.IsOrganizationGateway() {
		return []uuid.UUID{endpoint.ProjectID}, nil
	}
	if client.ProjectID.Valid {
		if endpoint.ProjectID != uuid.Nil && endpoint.ProjectID != client.ProjectID.UUID {
			return nil, nil
		}
		return []uuid.UUID{client.ProjectID.UUID}, nil
	}
	rows, err := metamcprepo.New(s.db).ListOrganizationMCPServersForRemoteSessionClient(ctx, metamcprepo.ListOrganizationMCPServersForRemoteSessionClientParams{
		OrganizationID:        endpoint.OrganizationID,
		ProjectID:             uuid.NullUUID{UUID: endpoint.ProjectID, Valid: endpoint.ProjectID != uuid.Nil},
		RemoteSessionClientID: client.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("list organization MCP servers for remote session client: %w", err)
	}
	surface := requestNetworkSurface(ctx)
	projectIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if !servableOnSurface(row.McpServerNetworkAccessMode, surface) {
			continue
		}
		if !slices.Contains(projectIDs, row.ProjectID) {
			projectIDs = append(projectIDs, row.ProjectID)
		}
	}
	return projectIDs, nil
}
