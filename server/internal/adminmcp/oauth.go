package adminmcp

import (
	"errors"
	"fmt"
	"log/slog"
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
	Clients              *StaffOAuthClients
	Authorization        *StaffOAuthAuthorization
	Tokens               *StaffOAuthTokens
	Approval             *StaffProposalApproval
	issuer               string
	resource             string
	protectedResourceURL string
	writes               WriteConfig
}

// NewStaffOAuth builds the staff OAuth handlers. The zero WriteConfig keeps
// every connection read-only: consent refuses admin:write unless at least one
// write operation is switched on.
func NewStaffOAuth(baseURL *url.URL, db *pgxpool.Pool, challengeCache cache.Cache, verifier adminSessionVerifier, cipher *encryption.Client, signer *sessiontokens.Signer, writes WriteConfig, logger *slog.Logger) (*StaffOAuth, error) {
	if baseURL == nil || baseURL.Scheme != "https" || baseURL.Host == "" || (baseURL.Path != "" && baseURL.Path != "/") || db == nil || challengeCache == nil || verifier == nil || cipher == nil || signer == nil {
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
	protectedResourceURL, err := url.JoinPath(base.String(), ".well-known", "oauth-protected-resource", "admin-mcp")
	if err != nil {
		return nil, fmt.Errorf("build staff MCP resource metadata URL: %w", err)
	}
	clients := NewStaffOAuthClients(db)
	clientStore := clients.store
	authorization := NewStaffOAuthAuthorization(clientStore, postgresStaffAuthorizationStore{db: db}, challengeCache, verifier, cipher, resource)
	authorization.writes = writes
	return &StaffOAuth{
		Clients:              clients,
		Authorization:        authorization,
		Tokens:               NewStaffOAuthTokens(clientStore, postgresStaffGrantStore{db: db}, verifier, cipher, signer, issuer, resource),
		Approval:             newStaffProposalApproval(newProposalStore(db, logger), authorization, challengeCache, writes),
		issuer:               issuer,
		resource:             resource,
		protectedResourceURL: protectedResourceURL,
		writes:               writes,
	}, nil
}

func (s *StaffOAuth) scopesSupported() []string {
	if s.writes.WritesAvailable() {
		return []string{ScopeRead, ScopeWrite}
	}
	return []string{ScopeRead}
}

func (s *StaffOAuth) Issuer() string { return s.issuer }

func (s *StaffOAuth) Resource() string { return s.resource }

func (s *StaffOAuth) ProtectedResourceURL() string { return s.protectedResourceURL }

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
	mux.Handle("GET", Path+"/proposals/{id}", s.handler(s.Approval.Handler()))
	mux.Handle("POST", Path+"/proposals/{id}", s.handler(s.Approval.Handler()))
}

func (*StaffOAuth) handler(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }
}

func (s *StaffOAuth) ProtectedResourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staffJSON(w, http.StatusOK, map[string]any{"resource": s.resource, "authorization_servers": []string{s.issuer}, "bearer_methods_supported": []string{"header"}, "scopes_supported": s.scopesSupported()})
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
			"scopes_supported":                      s.scopesSupported(),
		})
	})
}
