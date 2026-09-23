package adminmcp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// StaffOAuth groups the staff-only authorization server handlers. Composition
// does not attach them to a public mux; ingress remains a deployment decision.
type StaffOAuth struct {
	Clients       *StaffOAuthClients
	Authorization *StaffOAuthAuthorization
	Tokens        *StaffOAuthTokens
	issuer        string
	resource      string
}

func NewStaffOAuth(baseURL *url.URL, db *pgxpool.Pool, challengeCache cache.Cache, verifier adminSessionVerifier, cipher *encryption.Client, signer *sessiontokens.Signer) (*StaffOAuth, error) {
	if baseURL == nil || baseURL.Scheme != "https" || baseURL.Host == "" || db == nil || challengeCache == nil || verifier == nil || cipher == nil || signer == nil {
		return nil, errors.New("staff OAuth configuration is incomplete")
	}
	base := *baseURL
	base.RawQuery = ""
	base.Fragment = ""
	issuer, err := url.JoinPath(base.String(), "admin-mcp", "oauth")
	if err != nil {
		return nil, fmt.Errorf("build staff OAuth issuer: %w", err)
	}
	resource, err := url.JoinPath(base.String(), "admin-mcp")
	if err != nil {
		return nil, fmt.Errorf("build staff MCP resource: %w", err)
	}
	clients := NewStaffOAuthClients(db)
	clientStore := clients.store
	return &StaffOAuth{
		Clients:       clients,
		Authorization: NewStaffOAuthAuthorization(clientStore, postgresStaffAuthorizationStore{db: db}, challengeCache, verifier, cipher, resource),
		Tokens:        NewStaffOAuthTokens(clientStore, postgresStaffGrantStore{db: db}, verifier, cipher, signer, issuer, resource),
		issuer:        issuer,
		resource:      resource,
	}, nil
}

func (s *StaffOAuth) Attach(mux interface {
	Handle(string, string, http.HandlerFunc)
}) {
	mux.Handle("GET", "/.well-known/oauth-protected-resource/admin-mcp", s.handler(s.ProtectedResourceHandler()))
	mux.Handle("GET", "/.well-known/oauth-authorization-server/admin-mcp/oauth", s.handler(s.AuthorizationServerHandler()))
	mux.Handle("POST", Path+"/register", s.handler(s.Clients.RegisterHandler()))
	mux.Handle("GET", Path+"/authorize", s.handler(s.Authorization.AuthorizeHandler()))
	mux.Handle("GET", Path+"/connect", s.handler(s.Authorization.ConnectHandler()))
	mux.Handle("POST", Path+"/connect", s.handler(s.Authorization.ConnectHandler()))
	mux.Handle("POST", Path+"/token", s.handler(s.Tokens.TokenHandler()))
}

func (*StaffOAuth) handler(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }
}

func (s *StaffOAuth) ProtectedResourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staffJSON(w, http.StatusOK, map[string]any{"resource": s.resource, "authorization_servers": []string{s.issuer}, "bearer_methods_supported": []string{"header"}})
	})
}

func (s *StaffOAuth) AuthorizationServerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staffJSON(w, http.StatusOK, map[string]any{
			"issuer":                                s.issuer,
			"authorization_endpoint":                s.resource + "/authorize",
			"token_endpoint":                        s.resource + "/token",
			"registration_endpoint":                 s.resource + "/register",
			"response_types_supported":              usersessions.SupportedResponseTypes,
			"grant_types_supported":                 staffGrantTypes,
			"token_endpoint_auth_methods_supported": staffAuthMethods,
			"code_challenge_methods_supported":      usersessions.SupportedCodeChallengeMethods,
		})
	})
}
