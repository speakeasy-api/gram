package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
)

// registerErrorTestClient exercises anonymous DCR rather than assigning trust to
// a test-seeded database row. The same registration route serves every issuer.
func registerErrorTestClient(t *testing.T, ctx context.Context, handler http.Handler, path, redirectURI string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"client_name": "Test client", "redirect_uris": []string{redirectURI}, "token_endpoint_auth_method": "none"})
	require.NoError(t, err)
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, path+"/register", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var result struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	return result.ClientID
}

func TestAuthorize_PreAuthenticationErrors(t *testing.T) {
	t.Parallel()
	for _, client := range []struct {
		name, redirectURI string
		cimd              bool
		status            int
	}{
		{"DCR HTTPS", "https://client.example/cb?keep=value", false, http.StatusBadRequest},
		{"DCR custom scheme", "com.example.app://callback?keep=value", false, http.StatusBadRequest},
		{"CIMD HTTPS", "https://client.example/cb?keep=value", true, http.StatusBadRequest},
		{"DCR loopback HTTP", "http://localhost:33418/cb?keep=value", false, http.StatusFound},
		{"DCR loopback HTTPS", "https://[::1]:33418/cb?keep=value", false, http.StatusFound},
		{"CIMD loopback", "https://127.0.0.1:33418/cb?keep=value", true, http.StatusFound},
	} {
		for _, failure := range []struct{ name, field, value, code string }{
			{"response type", "response_type", "token", "unsupported_response_type"},
			{"missing PKCE", "code_challenge", "", "invalid_request"},
			{"PKCE method", "code_challenge_method", "plain", "invalid_request"},
			{"resource", "resource", "https://other.example/mcp", "invalid_target"},
		} {
			t.Run(client.name+"/"+failure.name, func(t *testing.T) {
				t.Parallel()
				ctx, ti, ds, toolset := newTestCIMDService(t)
				router := goahttp.NewMuxer()
				mcp.Attach(router, ti.service.Service, nil)
				path := "/mcp/" + toolset.McpSlug.String
				clientID := ds.clientID
				if client.cimd {
					ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{client.redirectURI} })
				} else {
					clientID = registerErrorTestClient(t, ctx, router, path, client.redirectURI)
				}
				query := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {client.redirectURI}, "state": {"state&<value>"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
				query.Set(failure.field, failure.value)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, path+"/authorize?"+query.Encode(), nil))
				callback := oauthtest.RequireAuthorizationErrorCallback(t, response, client.status)
				require.Equal(t, failure.code, callback.Query().Get("error"))
				require.NotEmpty(t, callback.Query().Get("error_description"))
				require.Equal(t, "state&<value>", callback.Query().Get("state"))
				require.Equal(t, "value", callback.Query().Get("keep"))
				issuer, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
				require.Equal(t, issuer, callback.Query().Get("iss"))
				registeredURI, err := url.Parse(client.redirectURI)
				require.NoError(t, err)
				require.Equal(t, registeredURI.Host, callback.Host)
				require.Equal(t, registeredURI.Scheme, callback.Scheme)
			})
		}
	}
}

func TestAuthorize_PreAuthenticationErrorRoutes(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"shared", "shared missing resource", "gateway", "authentication host", "shared authentication host"} {
		for _, client := range []struct {
			uri    string
			status int
		}{
			{"https://client.example/callback", http.StatusBadRequest},
			{"https://localhost:33418/callback", http.StatusFound},
		} {
			t.Run(route+"/"+client.uri, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				router := goahttp.NewMuxer()
				mcp.Attach(router, ti.service.Service, nil)
				var handler http.Handler = router
				var path, issuer, resource string
				errorCode := "unsupported_response_type"
				switch route {
				case "shared", "shared missing resource", "shared authentication host":
					f := seedSharedIssuer(t, ctx, ti)
					path = "/oauth/usi/" + f.issuer.ID.String()
					issuer, resource = f.issuerURL, f.resource(ti, f.slugA)
					if route == "shared missing resource" {
						resource = ""
						errorCode = "invalid_target"
					}
					if route == "shared authentication host" {
						issuer = testAuthenticationHostURL + path
						setIssuerMode(t, ctx, ti, f.issuer.ID, "shared", issuer)
						handler = newAuthenticationHostHarness(t, ti).handler
					}
				case "gateway":
					f := seedOrganizationGateway(t, ctx, ti)
					path = "/mcp/gateway/" + f.organizationSlug
					issuer = ti.serverURL.String() + path
				case "authentication host":
					f := seedAuthenticationHostEndpointIssuer(t, ctx, ti)
					path = "/mcp/" + f.slugA
					issuer = testAuthenticationHostURL + path
					handler = newAuthenticationHostHarness(t, ti).handler
				}
				// Absolute URLs select the authentication host when that is the issuer.
				clientID := registerErrorTestClient(t, ctx, handler, issuer, client.uri)
				query := url.Values{"response_type": {"token"}, "client_id": {clientID}, "redirect_uri": {client.uri}, "state": {"client-state"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
				if resource != "" {
					query.Set("resource", resource)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, issuer+"/authorize?"+query.Encode(), nil))
				callback := oauthtest.RequireAuthorizationErrorCallback(t, response, client.status)
				require.Equal(t, errorCode, callback.Query().Get("error"))
				require.Equal(t, issuer, callback.Query().Get("iss"))
				require.Equal(t, "client-state", callback.Query().Get("state"))
			})
		}
	}
}

func TestSharedAuthorize_PreAuthenticationCIMDResourceErrors(t *testing.T) {
	t.Parallel()
	for _, client := range []struct {
		uri    string
		status int
	}{
		{"https://client.example/callback", http.StatusBadRequest},
		{"https://localhost:33418/callback", http.StatusFound},
	} {
		for _, resources := range [][]string{nil, {"https://other.example/mcp"}, {"https://other.example/a", "https://other.example/b"}} {
			t.Run(client.uri+"/"+strings.Join(resources, ","), func(t *testing.T) {
				t.Parallel()
				ctx, ti, ds, toolset := newTestCIMDService(t)
				ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{client.uri} })
				// Resolve the document through the real authorize flow before switching
				// the issuer to shared mode. Resource rejection deliberately never fetches
				// an unknown client's metadata document.
				resolved := doCIMDAuthorize(t, ti, toolset.McpSlug.String, ds.clientID, client.uri, "")
				oauthtest.RequireAuthorizationErrorCallback(t, resolved, client.status)
				issuerID := toolset.UserSessionIssuerID.UUID
				setIssuerMode(t, ctx, ti, issuerID, "shared", "")
				issuer := ti.serverURL.String() + "/oauth/usi/" + issuerID.String()
				query := url.Values{"response_type": {"code"}, "client_id": {ds.clientID}, "redirect_uri": {client.uri}, "state": {"client-state"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}, "resource": resources}
				response := httptest.NewRecorder()
				require.NoError(t, ti.service.HandleSharedAuthorize(response, sharedRequest(ctx, http.MethodGet, issuer+"/authorize?"+query.Encode(), issuerID.String(), nil)))
				callback := oauthtest.RequireAuthorizationErrorCallback(t, response, client.status)
				require.Equal(t, "invalid_target", callback.Query().Get("error"))
				require.NotEmpty(t, callback.Query().Get("error_description"))
				require.Equal(t, issuer, callback.Query().Get("iss"))
				require.Equal(t, "client-state", callback.Query().Get("state"))
			})
		}
	}
}

func TestSharedAuthorize_UnknownCIMDResourceErrorHasNoCallback(t *testing.T) {
	t.Parallel()
	ctx, ti, ds, toolset := newTestCIMDService(t)
	issuerID := toolset.UserSessionIssuerID.UUID
	setIssuerMode(t, ctx, ti, issuerID, "shared", "")
	query := url.Values{"response_type": {"token"}, "client_id": {ds.clientID}, "redirect_uri": {"https://client.example/callback"}}
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedAuthorize(response, sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+issuerID.String()+"/authorize?"+query.Encode(), issuerID.String(), nil)))
	requireAuthorizeOAuthError(t, response, http.StatusBadRequest, "invalid_target")
	require.Empty(t, response.Header().Get("Location"))
	require.NotContains(t, response.Body.String(), "href=")
	require.Zero(t, ds.requests.Load())
}
