package localfixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOAuthHTTPPublishesSyntheticMetadataAndRegistersOnlyReviewedClient(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	handler := NewOAuthHTTP(config).Handler()

	metadataRequest := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/"+fixtureOAuthPath, nil)
	metadataResponse := httptest.NewRecorder()
	handler.ServeHTTP(metadataResponse, metadataRequest)
	require.Equal(t, http.StatusOK, metadataResponse.Code)
	require.Equal(t, "no-store", metadataResponse.Header().Get("Cache-Control"))
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(metadataResponse.Body.Bytes(), &metadata))
	require.Equal(t, config.OAuthIssuerURL(), metadata["issuer"])
	require.Equal(t, config.OAuthRegistrationURL(), metadata["registration_endpoint"])
	require.Equal(t, []any{"none"}, metadata["token_endpoint_auth_methods_supported"])
	require.Equal(t, []any{"S256"}, metadata["code_challenge_methods_supported"])

	body, err := json.Marshal(map[string]any{
		"client_name":                OAuthClientName,
		"redirect_uris":              []string{config.RemoteLoginCallbackURL()},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	require.NoError(t, err)
	registrationRequest := httptest.NewRequest(http.MethodPost, "/"+fixtureOAuthPath+"/register", bytes.NewReader(body))
	registrationRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	registrationResponse := httptest.NewRecorder()
	handler.ServeHTTP(registrationResponse, registrationRequest)
	require.Equal(t, http.StatusCreated, registrationResponse.Code)
	var registration map[string]any
	require.NoError(t, json.Unmarshal(registrationResponse.Body.Bytes(), &registration))
	clientID, ok := registration["client_id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, clientID)
	require.NotContains(t, registration, "client_secret")
	require.Equal(t, "none", registration["token_endpoint_auth_method"])
}

func TestOAuthHTTPAuthorizationCodeRefreshAndRevocationFlow(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	oauth := NewOAuthHTTP(config)
	handler := oauth.Handler()
	clientID := registerFixtureClient(t, handler, config)

	verifier := "fixture-code-verifier"
	code := authorizeFixtureClient(t, handler, config, clientID, verifier)
	initial := exchangeFixtureCode(t, handler, config, clientID, code, verifier)
	require.Equal(t, "Bearer", initial["token_type"])
	require.Equal(t, "tools:read", initial["scope"])
	refreshToken, ok := initial["refresh_token"].(string)
	require.True(t, ok)

	codeReplay := exchangeFixtureCodeResponse(t, handler, config, clientID, code, verifier)
	require.Equal(t, http.StatusBadRequest, codeReplay.Code)
	require.Equal(t, "no-store", codeReplay.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"error":"invalid_grant"}`, codeReplay.Body.String())

	invalidRefresh := postForm(t, handler, "/"+fixtureOAuthPath+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"wrong-client"},
		"resource":      {config.RemoteURL()},
	})
	require.Equal(t, http.StatusBadRequest, invalidRefresh.Code)
	require.Equal(t, "no-store", invalidRefresh.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"error":"invalid_grant"}`, invalidRefresh.Body.String())

	refresh := postForm(t, handler, "/"+fixtureOAuthPath+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"resource":      {config.RemoteURL()},
	})
	require.Equal(t, http.StatusOK, refresh.Code)
	var rotated map[string]any
	require.NoError(t, json.Unmarshal(refresh.Body.Bytes(), &rotated))
	rotatedAccessToken, ok := rotated["access_token"].(string)
	require.True(t, ok)
	rotatedRefreshToken, ok := rotated["refresh_token"].(string)
	require.True(t, ok)
	require.NotEqual(t, refreshToken, rotatedRefreshToken)

	staleRefresh := postForm(t, handler, "/"+fixtureOAuthPath+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"resource":      {config.RemoteURL()},
	})
	require.Equal(t, http.StatusBadRequest, staleRefresh.Code)
	require.Equal(t, "no-store", staleRefresh.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"error":"invalid_grant"}`, staleRefresh.Body.String())

	revoke := postForm(t, handler, "/"+fixtureOAuthPath+"/revoke", url.Values{"token": {rotatedRefreshToken}})
	require.Equal(t, http.StatusOK, revoke.Code)
	require.Equal(t, "no-store", revoke.Header().Get("Cache-Control"))

	revokedRefresh := postForm(t, handler, "/"+fixtureOAuthPath+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rotatedRefreshToken},
		"client_id":     {clientID},
		"resource":      {config.RemoteURL()},
	})
	require.Equal(t, http.StatusBadRequest, revokedRefresh.Code)
	require.Equal(t, "no-store", revokedRefresh.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"error":"invalid_grant"}`, revokedRefresh.Body.String())

	oauth.mu.Lock()
	_, accessStillLive := oauth.accessTokens[rotatedAccessToken]
	oauth.mu.Unlock()
	require.False(t, accessStillLive)
}

func TestOAuthHTTPExpiredAccessTokenPreservesLiveRefreshToken(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	oauth := NewOAuthHTTP(config)
	handler := oauth.Handler()
	clientID := registerFixtureClient(t, handler, config)
	code := authorizeFixtureClient(t, handler, config, clientID, "expired-access-verifier")
	tokens := exchangeFixtureCode(t, handler, config, clientID, code, "expired-access-verifier")
	accessToken, ok := tokens["access_token"].(string)
	require.True(t, ok)
	refreshToken, ok := tokens["refresh_token"].(string)
	require.True(t, ok)

	oauth.mu.Lock()
	issued := oauth.accessTokens[accessToken]
	issued.accessExpiresAt = time.Now().Add(-time.Second)
	oauth.accessTokens[accessToken] = issued
	oauth.refreshTokens[refreshToken] = issued
	oauth.mu.Unlock()

	require.False(t, oauth.HasLiveAccessToken(accessToken))
	refresh := postForm(t, handler, "/"+fixtureOAuthPath+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"resource":      {config.RemoteURL()},
	})
	require.Equal(t, http.StatusOK, refresh.Code)
}

func TestOAuthHTTPRestoresValidatedPersistedClient(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	oauth := NewOAuthHTTP(config)
	handler := oauth.Handler()

	require.NoError(t, oauth.RestoreRegisteredClient("persisted-local-client"))
	code := authorizeFixtureClient(t, handler, config, "persisted-local-client", "restored-client-verifier")
	tokens := exchangeFixtureCode(t, handler, config, "persisted-local-client", code, "restored-client-verifier")
	require.NotEmpty(t, tokens["access_token"])
}

func TestOAuthHTTPRejectsInvalidAuthorizationAndVerifier(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	handler := NewOAuthHTTP(config).Handler()
	clientID := registerFixtureClient(t, handler, config)

	invalidScope := httptest.NewRequest(http.MethodGet, "/"+fixtureOAuthPath+"/authorize?client_id="+url.QueryEscape(clientID)+"&redirect_uri="+url.QueryEscape(config.RemoteLoginCallbackURL())+"&response_type=code&state=state&scope=wrong&resource="+url.QueryEscape(config.RemoteURL())+"&code_challenge=challenge&code_challenge_method=S256", nil)
	invalidScopeResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidScopeResponse, invalidScope)
	require.Equal(t, http.StatusBadRequest, invalidScopeResponse.Code)

	code := authorizeFixtureClient(t, handler, config, clientID, "valid-verifier")
	wrongVerifier := exchangeFixtureCodeResponse(t, handler, config, clientID, code, "wrong-verifier")
	require.Equal(t, http.StatusBadRequest, wrongVerifier.Code)
	require.JSONEq(t, `{"error":"invalid_grant"}`, wrongVerifier.Body.String())

	consumedCode := exchangeFixtureCodeResponse(t, handler, config, clientID, code, "valid-verifier")
	require.Equal(t, http.StatusBadRequest, consumedCode.Code)
	require.JSONEq(t, `{"error":"invalid_grant"}`, consumedCode.Body.String())
}

func registerFixtureClient(t *testing.T, handler http.Handler, config *Config) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"client_name":                OAuthClientName,
		"redirect_uris":              []string{config.RemoteLoginCallbackURL()},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	require.NoError(t, err)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/"+fixtureOAuthPath+"/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)
	var registration map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &registration))
	clientID, ok := registration["client_id"].(string)
	require.True(t, ok)
	return clientID
}

func authorizeFixtureClient(t *testing.T, handler http.Handler, config *Config, clientID, verifier string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	requestURL := "/" + fixtureOAuthPath + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {config.RemoteLoginCallbackURL()},
		"response_type":         {"code"},
		"state":                 {"fixture-state"},
		"scope":                 {"tools:read"},
		"resource":              {config.RemoteURL()},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestURL, nil))
	require.Equal(t, http.StatusFound, response.Code)
	callback, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "fixture-state", callback.Query().Get("state"))
	return callback.Query().Get("code")
}

func exchangeFixtureCode(t *testing.T, handler http.Handler, config *Config, clientID, code, verifier string) map[string]any {
	t.Helper()
	response := exchangeFixtureCodeResponse(t, handler, config, clientID, code, verifier)
	require.Equal(t, http.StatusOK, response.Code)
	var tokens map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &tokens))
	return tokens
}

func exchangeFixtureCodeResponse(t *testing.T, handler http.Handler, config *Config, clientID, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, handler, "/"+fixtureOAuthPath+"/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {config.RemoteLoginCallbackURL()},
		"code_verifier": {verifier},
		"resource":      {config.RemoteURL()},
	})
}

func postForm(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestOAuthHTTPRejectsNonFixtureRegistration(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	handler := NewOAuthHTTP(config).Handler()

	body, err := json.Marshal(map[string]any{
		"client_name":                OAuthClientName,
		"redirect_uris":              []string{"https://other.example/callback"},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/"+fixtureOAuthPath+"/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.JSONEq(t, `{"error":"invalid_client_metadata","error_description":"request does not match the local fixture contract"}`, response.Body.String())

	wrongContentType := httptest.NewRequest(http.MethodPost, "/"+fixtureOAuthPath+"/register", bytes.NewReader(body))
	wrongContentType.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wrongContentTypeResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongContentTypeResponse, wrongContentType)
	require.Equal(t, http.StatusUnsupportedMediaType, wrongContentTypeResponse.Code)
}
