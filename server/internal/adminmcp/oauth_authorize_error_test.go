package adminmcp

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

func TestStaffOAuthPreAuthenticationErrors(t *testing.T) {
	t.Parallel()
	for _, client := range []struct {
		name, redirectURI string
		status            int
	}{
		{"DCR HTTPS", "https://client.example/cb?keep=value", http.StatusBadRequest},
		{"DCR custom scheme", "com.example.app:callback?keep=value", http.StatusBadRequest},
		{"loopback HTTP", "http://localhost:33418/cb?keep=value", http.StatusSeeOther},
		{"loopback HTTPS", "https://127.0.0.1:33418/cb?keep=value", http.StatusSeeOther},
	} {
		for _, failure := range []struct{ name, field, value, code string }{
			{"response type", "response_type", "token", "unsupported_response_type"},
			{"missing PKCE", "code_challenge", "", "invalid_request"},
			{"PKCE method", "code_challenge_method", "plain", "invalid_request"},
			{"PKCE format", "code_challenge", "bad", "invalid_request"},
			{"resource", "resource", "https://other.example/mcp", "invalid_target"},
			{"scope", "scope", "unavailable", "invalid_scope"},
		} {
			t.Run(client.name+"/"+failure.name, func(t *testing.T) {
				t.Parallel()
				service, store, _, pkce := staffAuthorizationFixture(t)
				clients := &recordingStaffClientStore{}
				service.clients = clients
				body, err := json.Marshal(map[string]any{"client_name": "Test client", "redirect_uris": []string{client.redirectURI}, "token_endpoint_auth_method": "none"})
				require.NoError(t, err)
				registration := httptest.NewRequest(http.MethodPost, Path+"/register", strings.NewReader(string(body)))
				registration.Header.Set("Content-Type", "application/json")
				registered := httptest.NewRecorder()
				(&StaffOAuthClients{store: clients}).RegisterHandler().ServeHTTP(registered, registration)
				require.Equal(t, http.StatusCreated, registered.Code, registered.Body.String())
				request := staffAuthorizeRequest(pkce)
				query := request.URL.Query()
				query.Set("client_id", clients.client.ID)
				query.Set("redirect_uri", client.redirectURI)
				query.Set("state", "state&<value>")
				query.Set(failure.field, failure.value)
				request.URL.RawQuery = query.Encode()
				response := httptest.NewRecorder()
				service.AuthorizeHandler().ServeHTTP(response, request)
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
				require.Zero(t, store.calls)
			})
		}
	}
}

func TestStaffOAuthUnregisteredLoopbackHasNoCallback(t *testing.T) {
	t.Parallel()
	service, _, _, pkce := staffAuthorizationFixture(t)
	request := staffAuthorizeRequest(pkce)
	query := request.URL.Query()
	query.Set("redirect_uri", "http://localhost:5556/callback")
	query.Set("response_type", "token")
	request.URL.RawQuery = query.Encode()
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Empty(t, response.Header().Get("Location"))
	require.NotContains(t, response.Body.String(), "href=")
}

func TestStaffOAuthPostAuthenticationErrorStillRedirectsRemoteCallback(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	staffRedirectError(response, httptest.NewRequest(http.MethodPost, Path+"/connect", nil), "https://client.example/cb", "client-state", &oauthwire.Error{Code: oautherr.CodeAccessDenied, Description: "authorization denied"})
	callback := oauthtest.RequireAuthorizationErrorCallback(t, response, http.StatusSeeOther)
	require.Equal(t, "client.example", callback.Host)
	require.Equal(t, oautherr.CodeAccessDenied, callback.Query().Get(oauthwire.ParamError))
	require.Equal(t, "client-state", callback.Query().Get(oauthwire.ParamState))
}
