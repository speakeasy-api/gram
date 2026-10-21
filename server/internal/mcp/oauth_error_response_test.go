package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

func TestOAuthInvalidClientPerServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	base := ti.serverURL.String() + "/mcp/" + toolset.McpSlug.String
	requireOAuthClientErrorResponses(t, router, base, base)
}

func TestOAuthInvalidClientPerServerAuthenticationHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, issuer.OrganizationID.String, issuer.ID)
	harness := newAuthenticationHostHarness(t, ti)
	path := "/mcp/" + toolset.McpSlug.String
	requireOAuthClientErrorResponses(t, harness.handler, testAuthenticationHostURL+path, ti.serverURL.String()+path)
}

func TestOAuthInvalidClientShared(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	requireOAuthClientErrorResponses(t, router, f.issuerURL, f.resource(ti, f.slugA))
}

func TestOAuthInvalidClientSharedAuthenticationHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	harness := newAuthenticationHostHarness(t, ti)
	base := authenticationHostSharedIssuerURL(f.issuer.ID.String())
	setIssuerMode(t, ctx, ti, f.issuer.ID, string(authserver.ModeShared), base)
	requireOAuthClientErrorResponses(t, harness.handler, base, f.resource(ti, f.slugA))
}

func TestOAuthInvalidClientOrganizationGateway(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedOrganizationGateway(t, ctx, ti)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	base := ti.serverURL.String() + "/mcp/gateway/" + f.organizationSlug
	requireOAuthClientErrorResponses(t, router, base, base)
}

func TestOAuthInvalidClientOrganizationGatewayAuthenticationHost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedOrganizationGateway(t, ctx, ti)
	useAuthenticationHost(t, ctx, ti, requireProjectAuthContext(t, ctx).ActiveOrganizationID, f.issuerID)
	harness := newAuthenticationHostHarness(t, ti)
	path := "/mcp/gateway/" + f.organizationSlug
	requireOAuthClientErrorResponses(t, harness.handler, testAuthenticationHostURL+path, ti.serverURL.String()+path)
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
			status     int
			challenge  string
		}{
			{name: "public client", formClient: true, status: http.StatusBadRequest},
			{name: "basic", headers: []string{"Basic dW5rbm93bjp3cm9uZw=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "lowercase basic", headers: []string{"basic dW5rbm93bjp3cm9uZw=="}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "malformed basic", headers: []string{"Basic invalid"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "basic without credentials", headers: []string{"Basic"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "basic and form", headers: []string{"Basic dW5rbm93bjp3cm9uZw=="}, formClient: true, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
			{name: "repeated basic", headers: []string{"Basic invalid", "Basic invalid"}, status: http.StatusUnauthorized, challenge: `Basic realm="Speakeasy"`},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				form := url.Values{oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamResource: {resource}, oauthwire.ParamToken: {"unknown-token"}}
				if tc.formClient {
					form.Set(oauthwire.ParamClientID, "unknown-client")
				}
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, base+"/"+endpoint, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				for _, value := range tc.headers {
					req.Header.Add("Authorization", value)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				require.Equal(t, tc.status, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), `"error":"invalid_client"`)
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
