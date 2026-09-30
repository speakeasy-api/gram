package platformmcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// testCanonicalBaseURL matches the base URL newTestOAuthHTTP configures.
	testCanonicalBaseURL = "https://gram.example"
	testExtraHostBaseURL = "https://ai.example"
)

func withRequestOrigin(r *http.Request, surface requestorigin.Surface, baseURL string) *http.Request {
	return r.WithContext(requestorigin.WithContext(r.Context(), requestorigin.Origin{
		Surface:          surface,
		BaseURL:          baseURL,
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}))
}

func TestOAuthHTTPMetadataFollowsPlatformHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		surface  requestorigin.Surface
		baseURL  string
		wantBase string
	}{
		{name: "no origin", surface: "", baseURL: "", wantBase: testCanonicalBaseURL},
		{name: "canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, wantBase: testCanonicalBaseURL},
		{name: "extra platform host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, wantBase: testExtraHostBaseURL},
		{name: "custom domain keeps configured base", surface: requestorigin.SurfaceCustomDomain, baseURL: "https://mcp.customer.example", wantBase: testCanonicalBaseURL},
		{name: "private network keeps configured base", surface: requestorigin.SurfacePrivateNetwork, baseURL: "https://private.example", wantBase: testCanonicalBaseURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := newTestOAuthHTTP(t)
			stamp := func(r *http.Request) *http.Request {
				if tc.surface == "" {
					return r
				}
				return withRequestOrigin(r, tc.surface, tc.baseURL)
			}
			resource := tc.wantBase + "/platform-mcp"

			protected := httptest.NewRecorder()
			service.ProtectedResourceHandler().ServeHTTP(protected, stamp(httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/platform-mcp", nil)))
			require.Equal(t, http.StatusOK, protected.Code)
			var protectedBody struct {
				Resource             string   `json:"resource"`
				AuthorizationServers []string `json:"authorization_servers"`
			}
			require.NoError(t, json.Unmarshal(protected.Body.Bytes(), &protectedBody))
			require.Equal(t, resource, protectedBody.Resource)
			require.Equal(t, []string{resource}, protectedBody.AuthorizationServers)

			server := httptest.NewRecorder()
			service.AuthorizationServerHandler().ServeHTTP(server, stamp(httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp", nil)))
			require.Equal(t, http.StatusOK, server.Code)
			var serverBody map[string]any
			require.NoError(t, json.Unmarshal(server.Body.Bytes(), &serverBody))
			require.Equal(t, resource, serverBody["issuer"])
			require.Equal(t, resource+"/authorize", serverBody["authorization_endpoint"])
			require.Equal(t, resource+"/token", serverBody["token_endpoint"])
			require.Equal(t, resource+"/register", serverBody["registration_endpoint"])
			require.Equal(t, resource+"/revoke", serverBody["revocation_endpoint"])
		})
	}
}

func TestRuntimeChallengeFollowsPlatformHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		surface requestorigin.Surface
		baseURL string
		want    string
	}{
		{name: "canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, want: testCanonicalBaseURL},
		{name: "extra platform host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, want: testExtraHostBaseURL},
		{name: "custom domain keeps configured base", surface: requestorigin.SurfaceCustomDomain, baseURL: "https://mcp.customer.example", want: testCanonicalBaseURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: testPrincipal()}, testGate{enabled: true}, &testAuthorizer{}, testCanonicalBaseURL+"/.well-known/oauth-protected-resource/platform-mcp", "test-cursor-key", nil, nil, nil, nil, nil).Handler()
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, withRequestOrigin(httptest.NewRequest(http.MethodPost, Path, nil), tc.surface, tc.baseURL))

			require.Equal(t, http.StatusUnauthorized, res.Code)
			require.Equal(t, `Bearer resource_metadata="`+tc.want+`/.well-known/oauth-protected-resource/platform-mcp"`, res.Header().Get("WWW-Authenticate"))
		})
	}
}

// An authorization that starts on an extra platform host keeps the identity
// provider callback on the configured host, returns the browser to the extra
// host, and yields tokens bound to the extra host's resource that the
// canonical host's token endpoint refuses to redeem or refresh.
func TestOAuthHTTPAuthorizationOnExtraPlatformHost(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	onExtra := func(r *http.Request) *http.Request {
		return withRequestOrigin(r, requestorigin.SurfacePlatform, testExtraHostBaseURL)
	}
	onCanonical := func(r *http.Request) *http.Request {
		return withRequestOrigin(r, requestorigin.SurfacePlatform, testCanonicalBaseURL)
	}
	verifier := strings.Repeat("v", 43)
	digest := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(digest[:])

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, onExtra(httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge="+codeChallenge+"&code_challenge_method=S256", nil)))
	require.Equal(t, http.StatusFound, authorize.Code)
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	// The identity provider only knows the configured host's callback.
	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, onCanonical(httptest.NewRequest(http.MethodGet, "/platform-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil)))
	require.Equal(t, http.StatusFound, callback.Code)
	selectionURL, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, testExtraHostBaseURL+"/platform-mcp/select-organization", selectionURL.Scheme+"://"+selectionURL.Host+selectionURL.Path)

	selection := httptest.NewRecorder()
	service.OrganizationSelectionHandler().ServeHTTP(selection, onExtra(httptest.NewRequest(http.MethodGet, selectionURL.RequestURI(), nil)))
	require.Equal(t, http.StatusOK, selection.Code)
	state := selectionURL.Query().Get("state")
	csrfStart := strings.Index(selection.Body.String(), `name="csrf_token" value="`) + len(`name="csrf_token" value="`)
	csrf, _, _ := strings.Cut(selection.Body.String()[csrfStart:], `"`)

	selected := httptest.NewRecorder()
	selectionRequest := httptest.NewRequest(http.MethodPost, "/platform-mcp/select-organization", strings.NewReader(url.Values{"state": {state}, "csrf_token": {csrf}, "organization_id": {"org-1"}}.Encode()))
	selectionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	service.OrganizationSelectionHandler().ServeHTTP(selected, onExtra(selectionRequest))
	require.Equal(t, http.StatusSeeOther, selected.Code)
	connectURL, err := url.Parse(selected.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, testExtraHostBaseURL+"/platform-mcp/connect", connectURL.Scheme+"://"+connectURL.Host+connectURL.Path)

	approve := httptest.NewRecorder()
	approveRequest := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(url.Values{"state": {state}, "csrf_token": {csrf}, "action": {"approve"}}.Encode()))
	approveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	service.ConnectHandler().ServeHTTP(approve, onExtra(approveRequest))
	require.Equal(t, http.StatusSeeOther, approve.Code)
	redirect, err := url.Parse(approve.Header().Get("Location"))
	require.NoError(t, err)
	code := redirect.Query().Get("code")
	require.NotEmpty(t, code)

	token := func(stamp func(*http.Request) *http.Request, form url.Values) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		service.TokenHandler().ServeHTTP(response, stamp(request))
		return response
	}
	codeForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://127.0.0.1:3000/callback"}, "code_verifier": {verifier}, "client_id": {"client-1"}}

	// Redeeming on another host is refused without consuming the code.
	wrongHost := token(onCanonical, codeForm)
	require.Equal(t, http.StatusBadRequest, wrongHost.Code)
	require.Contains(t, wrongHost.Body.String(), `"invalid_grant"`)

	exchanged := token(onExtra, codeForm)
	require.Equal(t, http.StatusOK, exchanged.Code, exchanged.Body.String())
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(exchanged.Body.Bytes(), &tokens))
	claims, err := service.signer.ValidateExactAudience(tokens.AccessToken, testExtraHostBaseURL+"/platform-mcp")
	require.NoError(t, err)
	require.Equal(t, testExtraHostBaseURL+"/platform-mcp", claims.Issuer)
	_, err = service.signer.ValidateExactAudience(tokens.AccessToken, testCanonicalBaseURL+"/platform-mcp")
	require.Error(t, err)

	refreshForm := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}, "client_id": {"client-1"}}
	wrongHostRefresh := token(onCanonical, refreshForm)
	require.Equal(t, http.StatusBadRequest, wrongHostRefresh.Code)
	require.Contains(t, wrongHostRefresh.Body.String(), `"invalid_grant"`)

	refreshed := token(onExtra, refreshForm)
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	var refreshedTokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &refreshedTokens))
	_, err = service.signer.ValidateExactAudience(refreshedTokens.AccessToken, testExtraHostBaseURL+"/platform-mcp")
	require.NoError(t, err)
}

// Refresh tokens issued before resource binding were all minted for the
// configured host, so they keep refreshing there and nowhere else.
func TestOAuthHTTPLegacyRefreshTokenStaysOnConfiguredHost(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	connection := platformoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "user:user-1", OrganizationID: "org-1", Generation: "generation-1", AuthorizationExpiresAt: service.now().Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	require.NoError(t, store.RegisterConnection(t.Context(), connection))
	refreshToken, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID, "")
	require.NoError(t, err)
	require.NoError(t, store.CreateSession(t.Context(), platformoauth.Session{ID: "session-1", ClientID: "client-1", Connection: connection, JTI: "jti-1", RefreshHash: opaqueHash(refreshToken), ExpiresAt: service.now().Add(platformAccessTokenLifetime), RefreshExpiresAt: service.now().Add(platformAccessTokenLifetime)}))

	refresh := func(baseURL string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {"client-1"}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		service.TokenHandler().ServeHTTP(response, withRequestOrigin(request, requestorigin.SurfacePlatform, baseURL))
		return response
	}

	wrongHost := refresh(testExtraHostBaseURL)
	require.Equal(t, http.StatusBadRequest, wrongHost.Code)
	require.Contains(t, wrongHost.Body.String(), `"invalid_grant"`)

	refreshed := refresh(testCanonicalBaseURL)
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &tokens))
	_, err = service.signer.ValidateExactAudience(tokens.AccessToken, testCanonicalBaseURL+"/platform-mcp")
	require.NoError(t, err)
}
