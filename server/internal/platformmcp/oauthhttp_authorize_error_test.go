package platformmcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
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

func TestOAuthHTTPConnectPostDenialStillRedirectsRemoteCallback(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"https://client.example/cb?keep=value"}}))
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: "https://client.example/cb?keep=value", State: "client-state", CSRFToken: "csrf", OrganizationID: "org-1", Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))

	form := url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"deny"}}
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, request)

	callback := oauthtest.RequireAuthorizationErrorCallback(t, response, http.StatusFound)
	require.Equal(t, "https", callback.Scheme)
	require.Equal(t, "client.example", callback.Host)
	require.Equal(t, "/cb", callback.Path)
	require.Equal(t, "value", callback.Query().Get("keep"))
	require.Equal(t, oautherr.CodeAccessDenied, callback.Query().Get(oauthwire.ParamError))
	require.Equal(t, "user denied authorization", callback.Query().Get(oauthwire.ParamErrorDescription))
	require.Equal(t, challenge.State, callback.Query().Get(oauthwire.ParamState))
}

func TestOAuthHTTPBrowserRoutesRejectMissingStateWithHTML(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/platform-mcp/idp_callback", http.StatusBadRequest},
		{http.MethodGet, "/platform-mcp/select-organization", http.StatusBadRequest},
		{http.MethodPost, "/platform-mcp/select-organization", http.StatusBadRequest},
		{http.MethodGet, "/platform-mcp/connect", http.StatusBadRequest},
		{http.MethodPost, "/platform-mcp/connect", http.StatusBadRequest},
		{http.MethodPost, "/platform-mcp/create-workspace", http.StatusUnauthorized},
		{http.MethodPost, "/platform-mcp/join-workspace", http.StatusUnauthorized},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()
			service := newTestOAuthHTTP(t)
			service.workspaces = &workspaceTestState{}
			mux := goahttp.NewMuxer()
			service.Attach(mux)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			mux.ServeHTTP(response, request)
			body := oauthtest.RequireAuthorizationErrorPage(t, response, tc.status)
			require.Equal(t, oautherr.CodeInvalidRequest, body.Code)
		})
	}
}

func TestOAuthHTTPClientValidationExplainsClientFailure(t *testing.T) {
	t.Parallel()
	for _, code := range []string{oautherr.CodeInvalidClient, oautherr.CodeInvalidClientMetadata, oautherr.CodeInvalidRedirectURI} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			service := newTestOAuthHTTP(t)
			response := httptest.NewRecorder()
			service.writeClientResolutionError(response, t.Context(), "https://client.example/client.json", &oauthwire.Error{Code: code, Description: "client validation failed"})
			body := oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusBadRequest)
			require.Equal(t, code, body.Code)
			require.Equal(t, "client validation failed", body.Description)
			require.Contains(t, response.Body.String(), "or its return address")
			require.NotContains(t, response.Body.String(), "started sign-in in a way")
		})
	}
}
