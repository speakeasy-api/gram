package mcp_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestOAuthInvalidClientPerServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	base := ti.serverURL.String() + "/mcp/" + toolset.McpSlug.String
	requireOAuthClientErrorResponses(t, router, base, base)
	requireOAuthMixedCredentialResponses(t, ti, issuer.ID, router, base, base)
}

func TestOAuthInvalidClientPerServerAuthenticationHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	harness := newAuthenticationHostHarness(t, ti)
	path := "/mcp/" + toolset.McpSlug.String
	requireOAuthClientErrorResponses(t, harness.handler, testAuthenticationHostURL+path, ti.serverURL.String()+path)
	requireOAuthMixedCredentialResponses(t, ti, issuer.ID, harness.handler, testAuthenticationHostURL+path, ti.serverURL.String()+path)
}

func TestOAuthInvalidClientShared(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	requireOAuthClientErrorResponses(t, router, f.issuerURL, f.resource(ti, f.slugA))
	requireOAuthMixedCredentialResponses(t, ti, f.issuer.ID, router, f.issuerURL, f.resource(ti, f.slugA))
}

func TestOAuthInvalidClientSharedAuthenticationHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	base := authenticationHostSharedIssuerURL(f.issuer.ID.String())
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), base)
	requireOAuthClientErrorResponses(t, harness.handler, base, f.resource(ti, f.slugA))
	requireOAuthMixedCredentialResponses(t, ti, f.issuer.ID, harness.handler, base, f.resource(ti, f.slugA))
}

func TestOAuthInvalidClientOrganizationGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedOrganizationGateway(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	base := ti.serverURL.String() + "/mcp/gateway/" + f.organizationSlug
	requireOAuthClientErrorResponses(t, router, base, base)
	requireOAuthMixedCredentialResponses(t, ti, f.issuerID, router, base, base)
}

func TestOAuthInvalidClientOrganizationGatewayAuthenticationHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedOrganizationGateway(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, requireProjectAuthContext(t, ctx).ActiveOrganizationID, f.issuerID)
	harness := newAuthenticationHostHarness(t, ti)
	path := "/mcp/gateway/" + f.organizationSlug
	requireOAuthClientErrorResponses(t, harness.handler, testAuthenticationHostURL+path, ti.serverURL.String()+path)
	requireOAuthMixedCredentialResponses(t, ti, f.issuerID, harness.handler, testAuthenticationHostURL+path, ti.serverURL.String()+path)
}

// requireOAuthClientErrorResponses exercises the token and revocation response
// contract through the mounted routes, including malformed Basic attempts.
func requireOAuthClientErrorResponses(t *testing.T, handler http.Handler, base, resource string) {
	t.Helper()

	for _, endpoint := range []string{"token", "revoke"} {
		for _, tc := range []struct {
			name       string
			headers    []string
			formClient bool
			code       string
			status     int
			challenge  string
		}{
			{name: "public client", formClient: true, status: http.StatusBadRequest},
			{name: "basic", headers: []string{"Basic dW5rbm93bjp3cm9uZw=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "lowercase basic", headers: []string{"basic dW5rbm93bjp3cm9uZw=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "malformed basic", headers: []string{"Basic invalid"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "basic without credentials", headers: []string{"Basic"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "basic and form", headers: []string{"Basic dW5rbm93bjp3cm9uZw=="}, formClient: true, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "repeated basic", headers: []string{"Basic invalid", "Basic invalid"}, status: http.StatusBadRequest, code: "invalid_request"},
			{name: "unsupported scheme", headers: []string{"Bearer token"}, status: http.StatusUnauthorized, challenge: `Bearer realm="Speakeasy"`},
			{name: "unsupported scheme with identifier", headers: []string{"Bearer token"}, formClient: true, status: http.StatusUnauthorized, challenge: `Bearer realm="Speakeasy"`},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				form := url.Values{oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamResource: {resource}, oauthwire.ParamToken: {"unknown-token"}}
				if tc.formClient {
					form.Set(oauthwire.ParamClientID, "unknown")
				}
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, base+"/"+endpoint, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				for _, value := range tc.headers {
					req.Header.Add("Authorization", value)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				require.Equal(t, tc.status, response.Code, response.Body.String())
				code := tc.code
				if code == "" {
					code = "invalid_client"
				}
				require.Contains(t, response.Body.String(), `"error":"`+code+`"`)
				require.Equal(t, tc.challenge, response.Header().Get("WWW-Authenticate"))
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				require.Empty(t, response.Header().Get("Location"))
			})
		}
	}
	t.Run("unsupported grant with basic", func(t *testing.T) {
		t.Parallel()
		request := httptest.NewRequest(http.MethodPost, base+"/token", strings.NewReader("grant_type=unsupported"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.SetBasicAuth("unknown-client", "wrong")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"error":"unsupported_grant_type"`)
		require.Empty(t, response.Header().Get("WWW-Authenticate"))
	})
}

func TestOAuthRejectsMultipleValidCredentials(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"token", "revoke"} {
		for _, tc := range []struct {
			name           string
			basic          bool
			repeatedHeader bool
			fields         url.Values
		}{
			{name: "repeated authorization", basic: true, repeatedHeader: true},
			{name: "basic and body secret", basic: true, fields: url.Values{"client_secret": {"test-secret"}}},
			{name: "basic and assertion", basic: true, fields: url.Values{"client_assertion": {"assertion"}, "client_assertion_type": {"jwt"}}},
			{name: "repeated client id", basic: true, fields: url.Values{"client_id": {"test-client", "test-client"}}},
			{name: "repeated secret", fields: url.Values{"client_secret": {"test-secret", "test-secret"}}},
			{name: "body secret and assertion", fields: url.Values{"client_secret": {"test-secret"}, "client_assertion": {"assertion"}}},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
				toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
				hash, err := bcrypt.GenerateFromPassword([]byte("test-secret"), bcrypt.MinCost)
				require.NoError(t, err)
				client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
					UserSessionIssuerID:     issuer.ID,
					ClientID:                "test-client",
					ClientName:              "Test client",
					ClientSecretHash:        pgtype.Text{String: string(hash), Valid: true},
					RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
					TokenEndpointAuthMethod: oauthwire.AuthMethodClientSecretBasic,
				})
				require.NoError(t, err)
				form := url.Values{oauthwire.ParamClientID: {client.ClientID}, oauthwire.ParamToken: {"unknown-token"}}
				if endpoint == "token" {
					code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
					form = codeGrantForm(client, code, verifier)
				}
				validForm := form.Encode()
				maps.Copy(form, tc.fields)
				router := goahttp.NewMuxer()
				mcp.Attach(router, ti.service.Service, nil)
				target := ti.serverURL.String() + "/mcp/" + toolset.McpSlug.String + "/" + endpoint
				request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if tc.basic {
					request.SetBasicAuth(client.ClientID, "test-secret")
				}
				if tc.repeatedHeader {
					request.Header.Add("Authorization", request.Header.Get("Authorization"))
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
				require.Empty(t, response.Header().Get("WWW-Authenticate"))
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))

				// Rejection leaves the grant usable by a single valid authentication method.
				request = httptest.NewRequest(http.MethodPost, target, strings.NewReader(validForm))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.SetBasicAuth(client.ClientID, "test-secret")
				response = httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			})
		}
	}
}

// requireOAuthMixedCredentialResponses exercises validation before grant
// dispatch and authentication across each mounted route, with both valid
// credentials and credentials that would fail client authentication.
func requireOAuthMixedCredentialResponses(t *testing.T, ti *testInstance, issuerID uuid.UUID, handler http.Handler, base, resource string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(t.Context(), usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "mixed-credential-client",
		ClientName:              "Test client",
		ClientSecretHash:        pgtype.Text{String: string(hash), Valid: true},
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		TokenEndpointAuthMethod: oauthwire.AuthMethodClientSecretBasic,
	})
	require.NoError(t, err)
	// A single valid presentation can revoke an unknown token on this route.
	request := httptest.NewRequest(http.MethodPost, base+"/revoke", strings.NewReader("token=unknown"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(client.ClientID, "test-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	for _, endpoint := range []string{"token", "revoke"} {
		grants := []string{""}
		if endpoint == "token" {
			grants = []string{oauthwire.GrantTypeJWTBearer, oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken, "unsupported"}
		}
		for _, grant := range grants {
			for _, tc := range []struct {
				name           string
				clientID       string
				secret         string
				repeatedHeader bool
				fields         url.Values
			}{
				{name: "repeated header", clientID: client.ClientID, secret: "test-secret", repeatedHeader: true},
				{name: "unknown client with repeated header", clientID: "unknown-client", secret: "test-secret", repeatedHeader: true},
				{name: "wrong secret with repeated header", clientID: client.ClientID, secret: "wrong-secret", repeatedHeader: true},
				{name: "body secret", clientID: client.ClientID, secret: "test-secret", fields: url.Values{oauthwire.ParamClientSecret: {"test-secret"}}},
				{name: "assertion", clientID: client.ClientID, secret: "test-secret", fields: url.Values{oauthwire.ParamClientAssertion: {"assertion"}, oauthwire.ParamClientAssertionType: {"jwt"}}},
				{name: "repeated identifier", clientID: client.ClientID, secret: "test-secret", fields: url.Values{oauthwire.ParamClientID: {client.ClientID, client.ClientID}}},
			} {
				t.Run("mixed/"+endpoint+"/"+grant+"/"+tc.name, func(t *testing.T) {
					t.Parallel()
					form := url.Values{oauthwire.ParamGrantType: {grant}, oauthwire.ParamResource: {resource}, oauthwire.ParamToken: {"unknown"}, oauthwire.ParamCode: {"unknown-code"}, oauthwire.ParamRefreshToken: {"unknown-refresh"}}
					maps.Copy(form, tc.fields)
					request := httptest.NewRequest(http.MethodPost, base+"/"+endpoint, strings.NewReader(form.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					request.SetBasicAuth(tc.clientID, tc.secret)
					if tc.repeatedHeader {
						request.Header.Add("Authorization", request.Header.Get("Authorization"))
					}
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
					require.Empty(t, response.Header().Get("WWW-Authenticate"))
					require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				})
			}
		}
	}
}
