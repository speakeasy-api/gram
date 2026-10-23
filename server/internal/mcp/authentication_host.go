// The authentication host: a second host that serves OAuth authorization
// servers and nothing else, so authentication stays apart from the hosts that
// carry MCP traffic.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// AuthenticationHost routes requests addressed to a dedicated authentication
// host to the per-server OAuth authorization server endpoints and to the
// shared authorization servers whose issuer is on it.
//
// For per-server authorization servers, the host is an alias for issuers that
// opt in with user_session_issuers.use_authentication_host: every route it
// serves also serves on the MCP host, and the same handlers run with the
// resource still derived from the MCP host. Such an issuer announces the
// authentication host as its issuer whichever host a request arrives on, so
// the issuer a client recorded stays the issuer it sees. For any other issuer
// those routes serve nothing.
//
// A shared authorization server whose issuer is on the authentication host is
// served there alone, browser sign-in included. A shared authorization server
// on another host is not served here.
//
// MCP traffic and protected-resource metadata are not served, so the host
// can never become an API host by accident; every such path answers 404.
// Per-endpoint routes resolve without a custom-domain context, so only
// endpoints addressable on the platform host are reachable through them. A
// shared authorization server resolves the resource each request names,
// wherever it lives.
//
// An AuthenticationHost with no configured host routes nothing: its
// middleware passes every request through untouched.
type AuthenticationHost struct {
	host            string
	baseURL         string
	platformBaseURL string
	router          chi.Router
}

type authenticationHostContextKey struct{}

// NewAuthenticationHost validates the configured authentication host URL
// against the platform server URL. An empty rawURL disables the host.
func NewAuthenticationHost(rawURL string, serverURL *url.URL, environment string) (*AuthenticationHost, error) {
	router := chi.NewRouter()
	router.NotFound(http.NotFound)
	router.MethodNotAllowed(http.NotFound)
	authenticationHost := &AuthenticationHost{
		host:            "",
		baseURL:         "",
		platformBaseURL: strings.TrimSuffix(serverURL.String(), "/"),
		router:          router,
	}
	if rawURL == "" {
		return authenticationHost, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse authentication host url: %w", err)
	}
	switch {
	case parsed.Scheme != "https" && (environment != "local" || parsed.Scheme != "http"):
		return nil, errors.New("authentication host url must use https outside local development")
	case parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "":
		return nil, errors.New("authentication host url must not carry userinfo, query or fragment")
	case parsed.Path != "" && parsed.Path != "/":
		return nil, errors.New("authentication host url must not carry a path")
	}
	host, err := requestorigin.CanonicalHost(parsed.Host)
	if err != nil {
		return nil, fmt.Errorf("authentication host url host: %w", err)
	}
	platformHost, err := requestorigin.CanonicalHost(serverURL.Host)
	if err != nil {
		return nil, fmt.Errorf("server url host: %w", err)
	}
	if host == platformHost {
		return nil, errors.New("authentication host must differ from the server url host")
	}

	authenticationHost.host = host
	authenticationHost.baseURL = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
	return authenticationHost, nil
}

// Host returns the canonical authentication host, or "" when it is disabled.
func (h *AuthenticationHost) Host() string {
	return h.host
}

// BaseURL returns the authentication host's base URL, or "" when it is
// disabled.
func (h *AuthenticationHost) BaseURL() string {
	return h.baseURL
}

// Middleware diverts requests whose Host is the authentication host to the
// host's own router and passes every other request to next.
//
// It must run before custom-domain resolution, which refuses hosts it does not
// know, and it replaces that resolution for the authentication host with a
// platform origin so resource URLs derive from the MCP host.
func (h *AuthenticationHost) Middleware(next http.Handler) http.Handler {
	if h.host == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.Matches(r) {
			next.ServeHTTP(w, r)
			return
		}

		ctx := requestorigin.WithContext(r.Context(), requestorigin.Origin{
			Surface:          requestorigin.SurfacePlatform,
			BaseURL:          h.platformBaseURL,
			OrganizationID:   "",
			NetworkIngressID: uuid.Nil,
			NetworkIdentity:  nil,
		})
		ctx = context.WithValue(ctx, authenticationHostContextKey{}, h.baseURL)
		h.router.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Matches reports whether r is addressed to the authentication host.
func (h *AuthenticationHost) Matches(r *http.Request) bool {
	if h.host == "" {
		return false
	}
	host, err := requestorigin.CanonicalHost(r.Host)
	return err == nil && host == h.host
}

// Handle mounts a route on the authentication host. It is a no-op when the
// host is disabled.
func (h *AuthenticationHost) Handle(method, pattern string, handler http.HandlerFunc) {
	if h.host == "" {
		return
	}
	h.router.Method(method, pattern, handler)
}

// AttachAuthenticationHost mounts the /mcp and organization gateway
// authorization server routes and the shared authorization server routes the
// host serves on the authentication
// host, and lets service announce it for issuers that opt in. It is a no-op
// when the host is disabled.
//
// The IdP and upstream login callbacks are not mounted: they are registered
// against the platform host and always return there.
func AttachAuthenticationHost(host *AuthenticationHost, service *Service) {
	if host.host == "" {
		return
	}
	RecordAuthenticationHost(host, service)

	handle := func(method, pattern string, handler func(http.ResponseWriter, *http.Request) error) {
		host.Handle(method, pattern, oops.ErrHandle(service.logger, handler).ServeHTTP)
	}
	handle(http.MethodGet, wellknown.OAuthAuthorizationServerPath+PublicServerRoute, service.HandleGetAuthorizationServer)
	handle(http.MethodPost, PublicServerRoute+"/register", service.HandleRegister)
	handle(http.MethodGet, PublicServerRoute+"/authorize", service.HandleAuthorize)
	handle(http.MethodGet, PublicServerRoute+"/connect", service.HandleConsent)
	handle(http.MethodPost, PublicServerRoute+"/connect", service.HandleConsent)
	handle(http.MethodPost, PublicServerRoute+"/connect/remote-session", service.HandleConsentAction)
	handle(http.MethodPost, PublicServerRoute+"/connect/mcp", service.HandleConsentMCP)
	handle(http.MethodDelete, PublicServerRoute+"/connect/mcp", service.HandleConsentMCP)
	handle(http.MethodGet, PublicServerRoute+"/connect/first-party", service.HandleFirstPartyConnect)
	handle(http.MethodGet, "/mcp/consent-page-{hash}.js", service.ServeConsentScript)
	handle(http.MethodGet, "/mcp/consent-tools-{hash}.js", service.ServeConsentToolsScript)
	handle(http.MethodGet, "/mcp/consent-fonts/{file}", service.ServeConsentFont)
	handle(http.MethodPost, PublicServerRoute+"/token", service.HandleToken)
	handle(http.MethodPost, PublicServerRoute+"/revoke", service.HandleRevoke)
	for _, route := range service.sharedAuthorizationServerRoutes() {
		handle(route.method, route.path, route.handler)
	}
	attachOrganizationGatewayAuthenticationHost(host, service)
	attachBuiltinAuthenticationHost(host, service)
}

// RecordAuthenticationHost records the authentication host on service without
// mounting any route on it, so a service that serves no authentication host
// routes still derives the same authorization servers as one that does, such
// as the shared authorization server of an issuer pinned to the host. It is a
// no-op when the host is disabled.
func RecordAuthenticationHost(host *AuthenticationHost, service *Service) {
	service.authenticationHostBaseURL = host.baseURL
}

// authenticationHostBaseURL reports the authentication host's base URL when
// the request arrived on the authentication host.
func authenticationHostBaseURL(ctx context.Context) (string, bool) {
	baseURL, ok := ctx.Value(authenticationHostContextKey{}).(string)
	return baseURL, ok && baseURL != ""
}

// withoutAuthenticationHost derives a context that reads as not having
// arrived on the authentication host.
func withoutAuthenticationHost(ctx context.Context) context.Context {
	return context.WithValue(ctx, authenticationHostContextKey{}, "")
}

// OnAuthenticationHost reports whether the request arrived on the
// authentication host.
func OnAuthenticationHost(ctx context.Context) bool {
	_, ok := authenticationHostBaseURL(ctx)
	return ok
}

// authorizationServerBaseURL is the origin of the endpoint's authorization
// server when its resource is served under resourceBaseURL: the
// authentication host when the endpoint's issuer opts in, otherwise the
// resource's own origin.
//
// Only a resource on the platform host moves. A custom domain or private
// network origin is an address the authentication host cannot stand in for.
func (s *Service) authorizationServerBaseURL(endpoint *ResolvedMcpEndpoint, resourceBaseURL string) string {
	if s.authenticationHostBaseURL == "" || !endpoint.useAuthenticationHost || endpoint.CustomDomainID.Valid {
		return resourceBaseURL
	}
	if strings.TrimSuffix(resourceBaseURL, "/") != strings.TrimSuffix(s.serverURL.String(), "/") {
		return resourceBaseURL
	}
	return s.authenticationHostBaseURL
}

// issuerURL is the issuer identifier of the authorization server serving the
// endpoint's OAuth request when its resource is served under resourceBaseURL.
// It is the AS metadata issuer, the RFC 9207 `iss` on authorization responses
// and the `iss` claim of minted tokens: the shared authorization server's fixed
// issuer when it is serving the request, otherwise the endpoint's own.
func (s *Service) issuerURL(endpoint *ResolvedMcpEndpoint, resourceBaseURL string) (string, error) {
	if shared := endpoint.servingSharedAuthorizationServer(); shared != nil {
		return shared.issuer, nil
	}
	issuer, err := endpoint.RootURL(s.authorizationServerBaseURL(endpoint, resourceBaseURL))
	if err != nil {
		return "", fmt.Errorf("build issuer URL: %w", err)
	}
	return issuer, nil
}

// arrivedAtAuthorizationServer reports whether the request arrived on the
// host the endpoint's issuer names. RFC 8414 §3.3 requires the issuer in a
// metadata document to match the URL it was retrieved from, so the document
// is served on that host alone, and a browser flow starts there.
func (s *Service) arrivedAtAuthorizationServer(ctx context.Context, endpoint *ResolvedMcpEndpoint, resourceBaseURL string) bool {
	arrivedAt := resourceBaseURL
	if baseURL, ok := authenticationHostBaseURL(ctx); ok {
		arrivedAt = baseURL
	}
	return s.authorizationServerBaseURL(endpoint, resourceBaseURL) == arrivedAt
}

// requestAuthorizationServerURLs is AuthorizationServerURLs for the host the
// request arrived on. Issuer is the endpoint's issuer; Token and Revoke name
// the host the request was sent to. That keeps an assertion's accepted
// audiences an exact pair for the addressed endpoint: the issuer, or the URL
// the request was sent to.
//
// A shared authorization server is served only on its issuer's host, so its
// own URLs are already the addressed ones.
func (s *Service) requestAuthorizationServerURLs(ctx context.Context, endpoint *ResolvedMcpEndpoint, resourceBaseURL string) (AuthorizationServerURLs, error) {
	if shared := endpoint.servingSharedAuthorizationServer(); shared != nil {
		return shared.urls()
	}
	urls, err := endpoint.AuthorizationServerURLs(s.authorizationServerBaseURL(endpoint, resourceBaseURL))
	if err != nil {
		return AuthorizationServerURLs{}, err
	}
	arrivedAt := resourceBaseURL
	if baseURL, ok := authenticationHostBaseURL(ctx); ok {
		arrivedAt = baseURL
	}
	addressed, err := endpoint.AuthorizationServerURLs(arrivedAt)
	if err != nil {
		return AuthorizationServerURLs{}, fmt.Errorf("build addressed authorization server URLs: %w", err)
	}
	urls.Token = addressed.Token
	urls.Revoke = addressed.Revoke
	return urls, nil
}

// consentURL is the consent page URL for the challenge stateID: on the shared
// authorization server when it is serving the endpoint's OAuth request,
// otherwise under perEndpointBaseURL, the origin the caller serves the
// endpoint's own consent page from.
func (s *Service) consentURL(endpoint *ResolvedMcpEndpoint, perEndpointBaseURL, stateID string) (string, error) {
	if shared := endpoint.servingSharedAuthorizationServer(); shared != nil {
		return shared.consentURL(stateID)
	}
	return endpoint.ConsentURL(perEndpointBaseURL, stateID)
}

// challengeAuthorizationServerBaseURL is the origin of the authorization
// server serving the challenge's flow: the shared authorization server's when
// it minted the challenge, otherwise the endpoint's own for the resource
// origin the challenge was minted under. The flow's consent page, and the
// browser binding it checks, live on this origin, which the authentication
// host makes differ from the resource origin.
func (s *Service) challengeAuthorizationServerBaseURL(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) string {
	if shared := endpoint.servingSharedAuthorizationServer(); shared != nil {
		return shared.origin()
	}
	return s.authorizationServerBaseURL(endpoint, state.mintOriginOr(s.serverURL.String()))
}

// challengeConsentURL is the consent page URL of the challenge state, on the
// origin of the authorization server serving its flow.
func (s *Service) challengeConsentURL(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (string, error) {
	return s.consentURL(endpoint, s.challengeAuthorizationServerBaseURL(endpoint, state), state.ID)
}

// remoteLoginConsentURL is the consent page an upstream login started from
// the challenge state's consent page returns to, or "" when that is the
// per-endpoint consent page on the resource origin. Remote sessions rebuild
// that one from the slug and revalidate the origin as they do, so it is named
// only when the consent page is elsewhere: on a shared authorization server,
// or on the authentication host.
func (s *Service) remoteLoginConsentURL(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (string, error) {
	if endpoint.servingSharedAuthorizationServer() == nil && s.challengeAuthorizationServerBaseURL(endpoint, state) == state.mintOriginOr(s.serverURL.String()) {
		return "", nil
	}
	return s.challengeConsentURL(endpoint, state)
}

// redirectToAuthorizationServer sends a browser flow that started on a host
// other than its authorization server's to the same request there, and
// reports whether it did. The flow binds the browser with a cookie on the host
// it starts on, which the consent page on the authorization server's host
// checks, so the two must be the same host. Only a per-endpoint authorization
// server of an issuer that opts in to the authentication host is addressable
// elsewhere: its routes also serve on the MCP host, where the dashboard and
// clients holding older metadata start flows.
func (s *Service) redirectToAuthorizationServer(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) bool {
	// The redirect only ever targets the authentication host, so a request
	// already there never redirects, however its base URLs compare.
	if OnAuthenticationHost(r.Context()) || endpoint.servingSharedAuthorizationServer() != nil {
		return false
	}
	resourceBaseURL := s.BaseURLForRequest(r)
	if s.arrivedAtAuthorizationServer(r.Context(), endpoint, resourceBaseURL) {
		return false
	}
	target := strings.TrimSuffix(s.authorizationServerBaseURL(endpoint, resourceBaseURL), "/") + r.URL.RequestURI()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
	return true
}

// protectedResourceAuthorizationServer is the authorization server the
// endpoint's RFC 9728 protected resource metadata names when its resource is
// served under resourceBaseURL: the issuer's shared authorization server when
// it has one, otherwise the endpoint's own. Requests over a private network
// ingress keep the endpoint's own authorization server on the private origin,
// which the shared authorization server does not serve.
func (s *Service) protectedResourceAuthorizationServer(ctx context.Context, endpoint *ResolvedMcpEndpoint, resourceBaseURL string) (string, error) {
	origin, ok := requestorigin.FromContext(ctx)
	private := ok && origin.Surface == requestorigin.SurfacePrivateNetwork
	if shared := endpoint.sharedAuthorizationServer; shared != nil && !private {
		return shared.issuer, nil
	}
	return s.issuerURL(endpoint, resourceBaseURL)
}
