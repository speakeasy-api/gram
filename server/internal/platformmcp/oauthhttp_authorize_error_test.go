package platformmcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

func TestOAuthHTTPPreAuthenticationErrors(t *testing.T) {
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
		} {
			t.Run(client.name+"/"+failure.name, func(t *testing.T) {
				t.Parallel()
				service, ds := newTestCIMDOAuthHTTP(t)
				clientID := ds.clientID
				if client.cimd {
					ds.set(t, func(ds *cimdDocServer) { ds.doc["redirect_uris"] = []any{client.redirectURI} })
				} else {
					body, err := json.Marshal(map[string]any{"client_name": "Test client", "redirect_uris": []string{client.redirectURI}, "token_endpoint_auth_method": "none"})
					require.NoError(t, err)
					request := httptest.NewRequest(http.MethodPost, "/platform-mcp/register", strings.NewReader(string(body)))
					request.Header.Set("Content-Type", "application/json")
					registered := httptest.NewRecorder()
					service.RegisterHandler().ServeHTTP(registered, request)
					require.Equal(t, http.StatusCreated, registered.Code, registered.Body.String())
					var result struct {
						ClientID string `json:"client_id"`
					}
					require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &result))
					clientID = result.ClientID
				}
				query := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {client.redirectURI}, "state": {"state&<value>"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
				query.Set(failure.field, failure.value)
				response := httptest.NewRecorder()
				service.AuthorizeHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?"+query.Encode(), nil))
				callback := oauthtest.RequireAuthorizationErrorCallback(t, response, client.status)
				require.Equal(t, failure.code, callback.Query().Get("error"))
				require.NotEmpty(t, callback.Query().Get("error_description"))
				require.Equal(t, "state&<value>", callback.Query().Get("state"))
				require.Equal(t, "value", callback.Query().Get("keep"))
				require.Empty(t, callback.Query().Get("iss"))
				registeredURI, err := url.Parse(client.redirectURI)
				require.NoError(t, err)
				require.Equal(t, registeredURI.Host, callback.Host)
				require.Equal(t, registeredURI.Scheme, callback.Scheme)
			})
		}
	}
}

func TestOAuthHTTPUnregisteredLoopbackPortHasNoCallback(t *testing.T) {
	t.Parallel()
	service := newTestOAuthHTTP(t)
	body := `{"client_name":"Test client","redirect_uris":["https://localhost:33418/callback"],"token_endpoint_auth_method":"none"}`
	registration := httptest.NewRequest(http.MethodPost, "/platform-mcp/register", strings.NewReader(body))
	registration.Header.Set("Content-Type", "application/json")
	registered := httptest.NewRecorder()
	service.RegisterHandler().ServeHTTP(registered, registration)
	require.Equal(t, http.StatusCreated, registered.Code)
	var client struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &client))
	query := url.Values{"client_id": {client.ClientID}, "redirect_uri": {"https://localhost:33419/callback"}, "response_type": {"token"}}
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?"+query.Encode(), nil))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Empty(t, response.Header().Get("Location"))
	require.Contains(t, response.Body.String(), `"invalid_client"`)
	require.NotContains(t, response.Body.String(), "href=")
}

func TestOAuthHTTPPostAuthenticationErrorStillRedirectsRemoteCallback(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	redirectOAuthError(response, httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", nil), "https://client.example/cb", "client-state", &oauthwire.Error{Code: oautherr.CodeAccessDenied, Description: "user denied authorization"})
	callback := oauthtest.RequireAuthorizationErrorCallback(t, response, http.StatusFound)
	require.Equal(t, "client.example", callback.Host)
	require.Equal(t, oautherr.CodeAccessDenied, callback.Query().Get(oauthwire.ParamError))
	require.Equal(t, "client-state", callback.Query().Get(oauthwire.ParamState))
}
