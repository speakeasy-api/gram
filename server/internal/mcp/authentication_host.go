// The authentication host: a second host that serves the per-server OAuth
// authorization server and nothing else, so authentication stays apart from
// the hosts that carry MCP traffic.

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
// host to the per-server OAuth authorization server endpoints.
//
// The host is an alias: every route it serves also serves on the MCP host,
// and the same handlers run with the resource still derived from the MCP
// host. Which host an endpoint announces as its issuer is a per-issuer
// setting (user_session_issuers.use_authentication_host), never the host a
// request happened to arrive on, so the issuer a client recorded stays the
// issuer it sees.
//
// MCP traffic and protected-resource metadata are not served, so the host
// can never become an API host by accident; every such path answers 404.
// Resolution runs without a custom-domain context, so only endpoints
// addressable on the platform host are reachable through it.
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
		host, err := requestorigin.CanonicalHost(r.Host)
		if err != nil || host != h.host {
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

// Handle mounts a route on the authentication host. It is a no-op when the
// host is disabled.
func (h *AuthenticationHost) Handle(method, pattern string, handler http.HandlerFunc) {
	if h.host == "" {
		return
	}
	h.router.Method(method, pattern, handler)
}

// AttachAuthenticationHost mounts the /mcp authorization server routes on the
// authentication host and lets service announce it for issuers that opt in.
// It is a no-op when the host is disabled.
//
// The IdP and upstream login callbacks are not mounted: they are registered
// against the platform host and always return there.
func AttachAuthenticationHost(host *AuthenticationHost, service *Service) {
	if host.host == "" {
		return
	}
	service.authenticationHostBaseURL = host.baseURL

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
}

// authenticationHostBaseURL reports the authentication host's base URL when
// the request arrived on the authentication host.
func authenticationHostBaseURL(ctx context.Context) (string, bool) {
	baseURL, ok := ctx.Value(authenticationHostContextKey{}).(string)
	return baseURL, ok && baseURL != ""
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

// issuerURL is the endpoint's OAuth issuer identifier when its resource is
// served under resourceBaseURL. It is the AS metadata issuer, the RFC 9207
// `iss` on authorization responses and the `iss` claim of minted tokens.
func (s *Service) issuerURL(endpoint *ResolvedMcpEndpoint, resourceBaseURL string) (string, error) {
	issuer, err := endpoint.RootURL(s.authorizationServerBaseURL(endpoint, resourceBaseURL))
	if err != nil {
		return "", fmt.Errorf("build issuer URL: %w", err)
	}
	return issuer, nil
}

// servesAuthorizationServerMetadata reports whether the request arrived on
// the host the endpoint's issuer names. RFC 8414 §3.3 requires the issuer in
// a metadata document to match the URL it was retrieved from, so the document
// is served on that host alone.
func (s *Service) servesAuthorizationServerMetadata(ctx context.Context, endpoint *ResolvedMcpEndpoint, resourceBaseURL string) bool {
	arrivedAt := resourceBaseURL
	if baseURL, ok := authenticationHostBaseURL(ctx); ok {
		arrivedAt = baseURL
	}
	return s.authorizationServerBaseURL(endpoint, resourceBaseURL) == arrivedAt
}

// requestAuthorizationServerURLs is AuthorizationServerURLs for the host the
// request arrived on. Issuer is the endpoint's issuer; Token and Revoke name
// the host the request was actually sent to. That keeps an assertion's
// accepted audiences an exact pair for the addressed endpoint: the issuer, or
// the URL the request was actually sent to.
func (s *Service) requestAuthorizationServerURLs(ctx context.Context, endpoint *ResolvedMcpEndpoint, resourceBaseURL string) (AuthorizationServerURLs, error) {
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
