// The token host: a second host that serves the per-server token endpoint and
// nothing else, so a client can keep its token exchange apart from the API
// hosts it sends bearer tokens to.

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

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// TokenHost routes requests addressed to a dedicated token host to the
// per-server token endpoint alone.
//
// The host is an alias: `POST /mcp/{mcpSlug}/token` also serves on the MCP
// host. On the token host the same handler runs with the
// canonical values (the issuer and the RFC 8707 resource) still derived from
// the MCP host, and only the token endpoint URL taken from the token host.
//
// Every other path and method on the token host answers 404, so the host can
// never serve MCP traffic and become an API host by accident. Resolution runs
// without a custom-domain context, so only endpoints addressable on the
// platform host are reachable through it.
//
// A TokenHost with no configured host routes nothing: its middleware passes
// every request through untouched.
type TokenHost struct {
	host            string
	baseURL         string
	platformBaseURL string
	router          chi.Router
}

type tokenHostContextKey struct{}

// NewTokenHost validates the configured token host URL against the platform
// server URL. An empty rawURL disables the token host.
func NewTokenHost(rawURL string, serverURL *url.URL, environment string) (*TokenHost, error) {
	router := chi.NewRouter()
	router.NotFound(http.NotFound)
	router.MethodNotAllowed(http.NotFound)
	tokenHost := &TokenHost{
		host:            "",
		baseURL:         "",
		platformBaseURL: strings.TrimSuffix(serverURL.String(), "/"),
		router:          router,
	}
	if rawURL == "" {
		return tokenHost, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse token host url: %w", err)
	}
	switch {
	case parsed.Scheme != "https" && (environment != "local" || parsed.Scheme != "http"):
		return nil, errors.New("token host url must use https outside local development")
	case parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "":
		return nil, errors.New("token host url must not carry userinfo, query or fragment")
	case parsed.Path != "" && parsed.Path != "/":
		return nil, errors.New("token host url must not carry a path")
	}
	host, err := requestorigin.CanonicalHost(parsed.Host)
	if err != nil {
		return nil, fmt.Errorf("token host url host: %w", err)
	}
	platformHost, err := requestorigin.CanonicalHost(serverURL.Host)
	if err != nil {
		return nil, fmt.Errorf("server url host: %w", err)
	}
	if host == platformHost {
		return nil, errors.New("token host must differ from the server url host")
	}

	tokenHost.host = host
	tokenHost.baseURL = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
	return tokenHost, nil
}

// Middleware diverts requests whose Host is the token host to the token host's
// own router and passes every other request to next.
//
// It must run before custom-domain resolution, which refuses hosts it does not
// know, and it replaces that resolution for the token host with a platform
// origin so canonical URLs derive from the MCP host.
func (t *TokenHost) Middleware(next http.Handler) http.Handler {
	if t.host == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, err := requestorigin.CanonicalHost(r.Host)
		if err != nil || host != t.host {
			next.ServeHTTP(w, r)
			return
		}

		ctx := requestorigin.WithContext(r.Context(), requestorigin.Origin{
			Surface:          requestorigin.SurfacePlatform,
			BaseURL:          t.platformBaseURL,
			OrganizationID:   "",
			NetworkIngressID: uuid.Nil,
			NetworkIdentity:  nil,
		})
		ctx = context.WithValue(ctx, tokenHostContextKey{}, t.baseURL)
		t.router.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AttachTokenHost mounts the token endpoint on the token host's router. It is
// a no-op when the token host is disabled.
func AttachTokenHost(tokenHost *TokenHost, service *Service) {
	if tokenHost.host == "" {
		return
	}
	tokenHost.router.Post(PublicServerRoute+"/token", oops.ErrHandle(service.logger, service.HandleToken).ServeHTTP)
}

// tokenHostBaseURL reports the token host's base URL when the request arrived
// on the token host.
func tokenHostBaseURL(ctx context.Context) (string, bool) {
	baseURL, ok := ctx.Value(tokenHostContextKey{}).(string)
	return baseURL, ok && baseURL != ""
}

// requestAuthorizationServerURLs is AuthorizationServerURLs for the host the
// request arrived on. Every value derives from baseURL, the MCP host, except
// Token, which names the token host when the request arrived there. That keeps
// an assertion's accepted audiences an exact pair for the addressed token
// endpoint: the issuer, or the URL the request was actually sent to.
func requestAuthorizationServerURLs(ctx context.Context, endpoint *ResolvedMcpEndpoint, baseURL string) (AuthorizationServerURLs, error) {
	urls, err := endpoint.AuthorizationServerURLs(baseURL)
	if err != nil {
		return AuthorizationServerURLs{}, err
	}
	tokenBaseURL, ok := tokenHostBaseURL(ctx)
	if !ok {
		return urls, nil
	}
	aliased, err := endpoint.AuthorizationServerURLs(tokenBaseURL)
	if err != nil {
		return AuthorizationServerURLs{}, fmt.Errorf("build token host URLs: %w", err)
	}
	urls.Token = aliased.Token
	return urls, nil
}
