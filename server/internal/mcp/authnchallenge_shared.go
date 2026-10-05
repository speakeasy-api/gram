// Shared authorization servers: one OAuth authorization server per user
// session issuer in shared mode, served at <origin>/oauth/usi/{id} for every
// MCP server attached to the issuer. Clients name the MCP server they want with
// an RFC 8707 resource indicator, and each access token is bound to that one
// server.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// sharedIssuerIDParam is the route parameter naming the issuer of a shared
// authorization server.
const sharedIssuerIDParam = "issuerID"

// sharedAuthorizationServerPattern is the chi route pattern every shared
// authorization server endpoint extends.
const sharedAuthorizationServerPattern = usersessions.SharedAuthorizationServerPathPrefix + "{" + sharedIssuerIDParam + "}"

// sharedResourceLogMaxBytes bounds how much of a rejected resource indicator
// is logged. Legitimate values are MCP server URLs well under this; the bound
// keeps a client from writing arbitrary amounts into the logs.
const sharedResourceLogMaxBytes = 512

// errSharedTokenIssuerMismatch: a token bound to an MCP server whose issuer is
// in shared mode named an authorization server other than that issuer's.
var errSharedTokenIssuerMismatch = errors.New("resource-bound token names another authorization server")

// sharedAuthorizationServer is the OAuth authorization server a user session
// issuer in shared mode serves for all of its MCP servers. Unlike a
// per-endpoint authorization server, its identity depends neither on the MCP
// server a request is for nor on the host the request arrived on.
type sharedAuthorizationServer struct {
	// issuerID is the user session issuer the authorization server belongs to.
	issuerID uuid.UUID

	// issuer is the RFC 8414 issuer identifier: the issuer's pinned issuer
	// URL, or <origin>/oauth/usi/{id} on the deployment when none is pinned.
	// It is also the `iss` of every token and authorization response the
	// authorization server issues.
	issuer string
}

// origin is the scheme and host the authorization server is served on.
func (a *sharedAuthorizationServer) origin() string {
	return urlOrigin(a.issuer)
}

// urls are the endpoints the authorization server advertises, rooted at its
// issuer identifier.
func (a *sharedAuthorizationServer) urls() (AuthorizationServerURLs, error) {
	urls := AuthorizationServerURLs{Issuer: a.issuer, Authorize: "", Token: "", Register: "", Revoke: ""}
	for _, p := range []struct {
		target *string
		suffix string
	}{
		{&urls.Authorize, "authorize"},
		{&urls.Token, "token"},
		{&urls.Register, "register"},
		{&urls.Revoke, "revoke"},
	} {
		u, err := url.JoinPath(a.issuer, p.suffix)
		if err != nil {
			return AuthorizationServerURLs{}, fmt.Errorf("build shared %s URL: %w", p.suffix, err)
		}
		*p.target = u
	}
	return urls, nil
}

// consentPath is the path of the authorization server's consent page.
func (a *sharedAuthorizationServer) consentPath() string {
	return usersessions.SharedAuthorizationServerPath(a.issuerID) + "/connect"
}

// consentURL is the consent page URL for the challenge stateID.
func (a *sharedAuthorizationServer) consentURL(stateID string) (string, error) {
	u, err := url.Parse(a.origin() + a.consentPath())
	if err != nil {
		return "", fmt.Errorf("parse shared consent URL: %w", err)
	}
	q := u.Query()
	q.Set("state", stateID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// sharedResource records where the MCP resource of an endpoint addressed
// through a shared authorization server lives. The shared authorization server
// is served on its own host, so the resource's origin cannot be read off the
// request the way a per-endpoint authorization server reads it.
type sharedResource struct {
	// origin is the request authority the request-origin middleware would stamp
	// on a request to the resource's own host.
	origin requestorigin.Origin
}

// servingSharedAuthorizationServer returns the shared authorization server
// serving the current OAuth request for this endpoint, or nil when its own
// per-endpoint authorization server is serving it.
func (e *ResolvedMcpEndpoint) servingSharedAuthorizationServer() *sharedAuthorizationServer {
	if e.sharedResource == nil {
		return nil
	}
	return e.sharedAuthorizationServer
}

// consentPath is the path of the consent page for the current OAuth request on
// the host that serves it.
func (e *ResolvedMcpEndpoint) consentPath() string {
	if shared := e.servingSharedAuthorizationServer(); shared != nil {
		return shared.consentPath()
	}
	return "/" + e.RouteBase + "/" + e.Slug + "/connect"
}

// SetPlatformHosts records the deployment's extra first-party hosts (from
// customdomains.ParsePlatformHosts), keyed by canonical host. A shared
// authorization server accepts resource indicators on them as it does on the
// server URL's own host.
func (s *Service) SetPlatformHosts(hosts map[string]string) {
	s.platformHosts = hosts
}

// issuerInSharedMode reports whether an issuer serves a shared authorization
// server.
func issuerInSharedMode(issuer usersessions_repo.UserSessionIssuer) bool {
	return usersessions.AuthorizationServerMode(issuer.AuthorizationServerMode) == usersessions.AuthorizationServerModeShared
}

// sharedAuthorizationServerFor builds the shared authorization server of an
// issuer in shared mode. The pinned issuer URL wins when set. Otherwise the
// issuer is derived: the authentication host when the issuer opts in to it and
// one is configured, else the server URL, followed by the issuer's path.
//
// An issuer naming a server this deployment does not serve is refused, so its
// MCP servers keep their per-endpoint authorization servers rather than
// pointing clients at a 404: a pinned issuer URL whose path is not the
// issuer's own, and one on a host the shared routes are not mounted on.
func (s *Service) sharedAuthorizationServerFor(issuer usersessions_repo.UserSessionIssuer) (*sharedAuthorizationServer, error) {
	if !issuerInSharedMode(issuer) {
		return nil, fmt.Errorf("user session issuer %s is not in shared mode", issuer.ID)
	}
	path := usersessions.SharedAuthorizationServerPath(issuer.ID)
	origin := ""
	if issuer.PinnedIssuerUrl.Valid && issuer.PinnedIssuerUrl.String != "" {
		pinned, err := url.Parse(issuer.PinnedIssuerUrl.String)
		if err != nil {
			return nil, fmt.Errorf("parse pinned issuer URL: %w", err)
		}
		if pinned.RawQuery != "" || pinned.Fragment != "" || pinned.Path != path {
			return nil, fmt.Errorf("pinned issuer URL %q is not this issuer's shared authorization server", issuer.PinnedIssuerUrl.String)
		}
		origin = urlOrigin(issuer.PinnedIssuerUrl.String)
	} else {
		base := s.serverURL.String()
		if issuer.UseAuthenticationHost && s.authenticationHostBaseURL != "" {
			base = s.authenticationHostBaseURL
		}
		origin = urlOrigin(base)
	}
	if !s.servesSharedAuthorizationServersOn(origin) {
		return nil, fmt.Errorf("shared authorization server issuer origin %q is not served by this deployment", origin)
	}
	return &sharedAuthorizationServer{issuerID: issuer.ID, issuer: origin + path}, nil
}

// servesSharedAuthorizationServersOn reports whether the shared authorization
// server routes are served on origin: the server URL's or an extra platform
// host's. Comparing whole origins also pins the scheme, so a pinned issuer is
// https wherever the deployment is.
//
// TODO(AIM-415): include the authentication host once it serves them.
func (s *Service) servesSharedAuthorizationServersOn(origin string) bool {
	if origin == "" {
		return false
	}
	if origin == urlOrigin(s.serverURL.String()) {
		return true
	}
	for _, baseURL := range s.platformHosts {
		if origin == urlOrigin(baseURL) {
			return true
		}
	}
	return false
}

// sharedIssuer is the issuer-level state of a request to a shared
// authorization server: what metadata, registration, and revocation read, none
// of which concern a particular MCP server. Requests that do concern one
// resolve a ResolvedMcpEndpoint from the resource they name.
type sharedIssuer struct {
	// authorizationServer is the issuer's shared authorization server.
	authorizationServer *sharedAuthorizationServer

	// cimdAdmissionModeRaw is the issuer's stored
	// client_id_metadata_admission_mode, carried verbatim for
	// admission.ResolveMode.
	cimdAdmissionModeRaw pgtype.Text

	// logger carries the issuer id for every line the request logs.
	logger *slog.Logger
}

// sharedIssuerForRequest resolves the shared authorization server a
// /oauth/usi/{id} request addresses. An unparseable or unknown id, an issuer
// in another mode or with a malformed pinned issuer, and a request on a host
// other than the issuer's all get the same 404, so the route cannot be used to
// tell which issuers exist.
func (s *Service) sharedIssuerForRequest(r *http.Request) (*sharedIssuer, error) {
	ctx := r.Context()
	notFound := oops.E(oops.CodeNotFound, nil, "authorization server not found")

	issuerID, err := uuid.Parse(chi.URLParam(r, sharedIssuerIDParam))
	if err != nil {
		return nil, notFound
	}
	logger := s.logger.With(attr.SlogUserSessionIssuerID(issuerID.String()))
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface != requestorigin.SurfacePlatform {
		return nil, notFound
	}

	row, err := usersessions_repo.New(s.db).GetSharedUserSessionIssuerByID(ctx, issuerID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, notFound
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load user session issuer").LogError(ctx, logger)
	}
	if !issuerInSharedMode(row.UserSessionIssuer) {
		return nil, notFound
	}
	authorizationServer, err := s.sharedAuthorizationServerFor(row.UserSessionIssuer)
	if err != nil {
		logger.ErrorContext(ctx, "shared authorization server is misconfigured", attr.SlogError(err))
		return nil, notFound
	}

	arrivedAt := s.BaseURLForRequest(r)
	if baseURL, ok := authenticationHostBaseURL(ctx); ok {
		arrivedAt = baseURL
	}
	if urlOrigin(arrivedAt) != authorizationServer.origin() {
		return nil, notFound
	}

	return &sharedIssuer{
		authorizationServer:  authorizationServer,
		cimdAdmissionModeRaw: row.UserSessionIssuer.ClientIDMetadataAdmissionMode,
		logger:               logger.With(attr.SlogOrganizationID(row.ResolvedOrganizationID)),
	}, nil
}

// sharedResourceRejection is why a shared authorization server refused an
// RFC 8707 resource indicator. The values are logged and form the closed set of
// the oauth.resource.rejected metric's reason dimension.
type sharedResourceRejection string

const (
	// sharedResourceAccepted: the resource resolved; not a rejection.
	sharedResourceAccepted sharedResourceRejection = ""

	// sharedResourceMissing: the request named no resource where one is
	// required.
	sharedResourceMissing sharedResourceRejection = "resource_missing"

	// sharedResourceMultiple: the request named more than one resource. Each
	// token is bound to exactly one MCP server.
	sharedResourceMultiple sharedResourceRejection = "resource_multiple"

	// sharedResourceMalformed: the resource is not an absolute URL of the
	// shape an MCP server has.
	sharedResourceMalformed sharedResourceRejection = "resource_malformed"

	// sharedResourceUnknownHost: the resource's host is neither a host of the
	// deployment nor an active custom domain.
	sharedResourceUnknownHost sharedResourceRejection = "resource_unknown_host"

	// sharedResourceNotFound: no MCP server with an OAuth surface answers at
	// the resource, on the public surface. It may not exist at all, which this
	// deliberately does not tell apart from belonging to another issuer.
	sharedResourceNotFound sharedResourceRejection = "resource_not_found"

	// sharedResourceForeignIssuer: the resource names an MCP server attached to
	// a different issuer.
	sharedResourceForeignIssuer sharedResourceRejection = "resource_foreign_issuer"

	// sharedResourceMismatch: a token request named a resource other than the
	// one its code or refresh token is bound to.
	sharedResourceMismatch sharedResourceRejection = "resource_mismatch"
)

// resolveSharedResourceIndicators resolves the resource indicators of a request
// to a shared authorization server to the one MCP server they name.
func (s *Service) resolveSharedResourceIndicators(ctx context.Context, logger *slog.Logger, authorizationServer *sharedAuthorizationServer, resources []string) (*ResolvedMcpEndpoint, sharedResourceRejection, error) {
	switch len(resources) {
	case 0:
		return nil, sharedResourceMissing, nil
	case 1:
		return s.resolveSharedResource(ctx, logger, authorizationServer, resources[0])
	default:
		return nil, sharedResourceMultiple, nil
	}
}

// resolveSharedResource resolves an RFC 8707 resource indicator to the MCP
// server it names and confirms the server is attached to the authorization
// server's issuer.
//
// The resource must be exactly the URL the server's protected resource metadata
// advertises, the same exact match per-endpoint authorization servers apply: no
// case folding, default port elision, or trailing slash fixups. It is resolved
// as a request to the resource's own host would be, with that host's surface
// and custom domain, and only on the public surface; MCP servers reached over a
// private network ingress keep their per-endpoint authorization servers.
func (s *Service) resolveSharedResource(ctx context.Context, logger *slog.Logger, authorizationServer *sharedAuthorizationServer, resource string) (*ResolvedMcpEndpoint, sharedResourceRejection, error) {
	parsed, err := url.Parse(resource)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return nil, sharedResourceMalformed, nil
	}
	host, err := requestorigin.CanonicalHost(parsed.Host)
	if err != nil {
		return nil, sharedResourceMalformed, nil
	}

	resourceCtx, origin, rejection, err := s.sharedResourceContext(ctx, host)
	if err != nil || rejection != sharedResourceAccepted {
		return nil, rejection, err
	}

	basePath := strings.TrimSuffix(strings.TrimPrefix(origin.BaseURL, urlOrigin(origin.BaseURL)), "/")
	routePath, ok := strings.CutPrefix(parsed.Path, basePath+"/")
	if !ok {
		return nil, sharedResourceMalformed, nil
	}
	routeBase := ""
	slug := ""
	switch {
	case strings.HasPrefix(routePath, "x/mcp/"):
		routeBase, slug = "x/mcp", strings.TrimPrefix(routePath, "x/mcp/")
	case strings.HasPrefix(routePath, "mcp/"):
		routeBase, slug = "mcp", strings.TrimPrefix(routePath, "mcp/")
	default:
		return nil, sharedResourceMalformed, nil
	}
	if slug == "" || strings.Contains(slug, "/") {
		return nil, sharedResourceMalformed, nil
	}

	endpoint, err := s.LoadResolvedMcpEndpointBySlug(resourceCtx, logger, slug, routeBase)
	if err != nil {
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) && shareable.Code == oops.CodeNotFound {
			return nil, sharedResourceNotFound, nil
		}
		return nil, sharedResourceAccepted, err
	}
	if endpoint.UserSessionIssuerID != authorizationServer.issuerID || endpoint.sharedAuthorizationServer == nil {
		return nil, sharedResourceForeignIssuer, nil
	}
	canonical, err := endpoint.RootURL(origin.BaseURL)
	if err != nil {
		return nil, sharedResourceAccepted, fmt.Errorf("build shared resource identifier: %w", err)
	}
	if resource != canonical {
		return nil, sharedResourceMalformed, nil
	}
	endpoint.sharedResource = &sharedResource{origin: origin}
	return endpoint, sharedResourceAccepted, nil
}

// sharedResourceContext derives the request context a request to the
// resource's host would carry: the request origin, and for a custom domain the
// customdomains.Context, that the request middleware would stamp. It mirrors
// customdomains.Middleware for the hosts it classifies.
func (s *Service) sharedResourceContext(ctx context.Context, host string) (context.Context, requestorigin.Origin, sharedResourceRejection, error) {
	var noOrigin requestorigin.Origin
	platform := func(baseURL string) (context.Context, requestorigin.Origin, sharedResourceRejection, error) {
		origin := requestorigin.Origin{
			Surface:          requestorigin.SurfacePlatform,
			BaseURL:          baseURL,
			OrganizationID:   "",
			NetworkIngressID: uuid.Nil,
			NetworkIdentity:  nil,
		}
		resourceCtx := requestorigin.WithContext(customdomains.WithContext(ctx, nil), origin)
		return resourceCtx, origin, sharedResourceAccepted, nil
	}

	serverHost, err := requestorigin.CanonicalHost(s.serverURL.Host)
	if err != nil {
		return nil, noOrigin, sharedResourceAccepted, fmt.Errorf("canonicalize server URL host: %w", err)
	}
	if host == serverHost {
		return platform(strings.TrimSuffix(s.serverURL.String(), "/"))
	}
	if baseURL, ok := s.platformHosts[host]; ok {
		return platform(baseURL)
	}

	domain, err := customdomains_repo.New(s.db).GetCustomDomainByDomain(ctx, host)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, noOrigin, sharedResourceUnknownHost, nil
	case err != nil:
		return nil, noOrigin, sharedResourceAccepted, fmt.Errorf("load custom domain for shared resource: %w", err)
	}
	if !domain.Activated || !domain.Verified {
		return nil, noOrigin, sharedResourceUnknownHost, nil
	}
	origin := requestorigin.Origin{
		Surface:          requestorigin.SurfaceCustomDomain,
		BaseURL:          "https://" + host,
		OrganizationID:   domain.OrganizationID,
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}
	resourceCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: domain.OrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	return requestorigin.WithContext(resourceCtx, origin), origin, sharedResourceAccepted, nil
}

// recordSharedResourceRejection logs and counts a refused resource indicator.
// The issuer is known by the time any of these can happen: requests for
// unknown issuers are refused before their resource is read, so the metric's
// issuer dimension only ever carries ids of real shared issuers.
func (s *Service) recordSharedResourceRejection(ctx context.Context, logger *slog.Logger, issuerID uuid.UUID, stage mcpmetrics.OAuthFlowStage, rejection sharedResourceRejection, resources []string) {
	logged := strings.Join(resources, ", ")
	if len(logged) > sharedResourceLogMaxBytes {
		logged = strings.ToValidUTF8(logged[:sharedResourceLogMaxBytes], "")
	}
	logger.InfoContext(ctx, "shared authorization server refused resource indicator",
		attr.SlogOAuthFailureReason(string(rejection)),
		attr.SlogOAuthFlowStage(string(stage)),
		attr.SlogOAuthResource(logged),
	)
	s.metrics.RecordOAuthResourceRejected(ctx, issuerID.String(), string(rejection), stage)
}

// recordSharedTokenResourceMismatch records a token request whose resource
// indicators named a server other than its grant's, when a shared
// authorization server is serving it. Per-endpoint token requests are logged
// by the grant handlers alone.
func (s *Service) recordSharedTokenResourceMismatch(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, resources []string) {
	if endpoint.servingSharedAuthorizationServer() == nil {
		return
	}
	s.recordSharedResourceRejection(ctx, logger, endpoint.UserSessionIssuerID, mcpmetrics.OAuthFlowStageToken, sharedResourceMismatch, resources)
}

// sharedSessionResource is the RFC 8707 resource a session minted for this
// endpoint is bound to: the endpoint's URL when the shared authorization server
// is serving the request, and "" for the per-endpoint authorization server,
// whose sessions are bound to the issuer instead.
func (s *Service) sharedSessionResource(endpoint *ResolvedMcpEndpoint) (string, error) {
	if endpoint.servingSharedAuthorizationServer() == nil {
		return "", nil
	}
	resource, err := endpoint.RootURL(endpoint.sharedResource.origin.BaseURL)
	if err != nil {
		return "", fmt.Errorf("build shared session resource: %w", err)
	}
	return resource, nil
}

// resourceBaseURL is the origin of the endpoint's MCP resource for an OAuth
// request: the resource's own origin when the shared authorization server is
// serving it, otherwise the origin the request arrived on.
func (s *Service) resourceBaseURL(r *http.Request, endpoint *ResolvedMcpEndpoint) string {
	if endpoint.sharedResource != nil {
		return endpoint.sharedResource.origin.BaseURL
	}
	return s.BaseURLForRequest(r)
}

// checkSharedResourceSession checks the issuer of a token accepted on the
// endpoint's exact resource audience, and reports whether the shared
// authorization server minted it, which makes it the one kind of
// resource-bound session that carries a refresh token.
//
// A token naming the endpoint's own per-endpoint authorization server, which
// mints its ID-JAG and workload sessions for the exact resource, is accepted
// as it always was, whatever the issuer's mode. Any other issuer is checked
// only for an endpoint whose issuer is in shared mode, and must be its shared
// authorization server. Both compare as the host check does, with the
// authentication host standing in for the server URL, so an issuer toggling
// use_authentication_host keeps its live tokens. Dashboard-minted tokens are
// exempt, as they are from the host check: their `iss` is descriptive rather
// than the host that minted them.
//
// The runtime gate builds toolset endpoints without loading their issuer, so
// this loads it only for a token naming neither the endpoint's own
// authorization server nor the dashboard, keeping the lookup off every token
// endpoint-mode issuers mint.
//
// TODO(AIM-402): report refreshability from how the session was issued once
// the shared token endpoint also mints ID-JAG and workload sessions, which
// will carry its issuer without carrying a refresh token.
func (s *Service) checkSharedResourceSession(ctx context.Context, session sessiontokens.ValidatedSession, endpoint *ResolvedMcpEndpoint, baseURL string) (bool, error) {
	if session.ClientID() == sessiontokens.FirstPartyClientID {
		return false, nil
	}
	perEndpointIssuer, err := s.issuerURL(endpoint, baseURL)
	if err != nil {
		return false, fmt.Errorf("build per-endpoint token issuer: %w", err)
	}
	if s.sameIssuer(session.Issuer(), perEndpointIssuer) {
		return false, nil
	}
	if !endpoint.issuerStamped {
		if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
			// An issuer that is gone takes its sessions with it.
			var shareable *oops.ShareableError
			if errors.As(err, &shareable) && shareable.Code == oops.CodeNotFound {
				return false, fmt.Errorf("%w: load issuer of resource-bound session: %w", errCredentialRejected, err)
			}
			return false, fmt.Errorf("load issuer of resource-bound session: %w", err)
		}
	}
	shared := endpoint.sharedAuthorizationServer
	if shared == nil {
		return false, nil
	}
	if s.sameIssuer(session.Issuer(), shared.issuer) {
		return true, nil
	}
	return false, fmt.Errorf("%w: %w: token issuer %q", errCredentialRejected, errSharedTokenIssuerMismatch, session.Issuer())
}

// sameIssuer reports whether two issuer URLs name the same authorization
// server: the same path on the same origin, counting the authentication host
// as the server URL it stands in for.
func (s *Service) sameIssuer(a, b string) bool {
	parsedA, errA := url.Parse(a)
	parsedB, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	originA := s.tokenMintOrigin(a)
	return originA != "" && originA == s.tokenMintOrigin(b) && parsedA.Path == parsedB.Path
}
