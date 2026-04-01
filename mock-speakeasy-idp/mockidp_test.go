package mockidp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockServer creates a test server in mock mode.
func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := NewConfig()
	srv := httptest.NewServer(Handler(cfg))
	t.Cleanup(srv.Close)
	return srv
}

// newOidcServer creates a test server in OIDC mode and returns both the
// httptest.Server and the underlying *server for injecting test state.
func newOidcServer(t *testing.T) (*httptest.Server, *server) {
	t.Helper()
	cfg := Config{
		SecretKey: "secret",
		Oidc: OidcConfig{
			Issuer:       "https://test.authkit.app/",
			ClientID:     "client_test",
			ClientSecret: "sk_test",
			ExternalURL:  "http://localhost:35291",
		},
	}
	s := &server{
		cfg:                cfg,
		authCodes:          make(map[string]authCodeEntry),
		tokens:             make(map[string]tokenEntry),
		userAdditionalOrgs: make(map[string][]organization),
		oidcPendingLogins:  make(map[string]pendingOidcLogin),
		oidcAuthCodes:      make(map[string]*oidcSession),
		oidcTokens:         make(map[string]*oidcSession),
		pendingLogouts:     make(map[string]pendingLogout),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/speakeasy_provider/login", s.handleLogin)
	mux.HandleFunc("GET /v1/speakeasy_provider/oidc/callback", s.handleOidcCallback)
	mux.HandleFunc("GET /v1/speakeasy_provider/logout/callback", s.handleLogoutCallback)
	mux.HandleFunc("POST /v1/speakeasy_provider/exchange", s.withAuth(s.handleExchange))
	mux.HandleFunc("GET /v1/speakeasy_provider/validate", s.withAuth(s.handleValidate))
	mux.HandleFunc("POST /v1/speakeasy_provider/revoke", s.withAuth(s.handleRevoke))
	mux.HandleFunc("POST /v1/speakeasy_provider/register", s.withAuth(s.handleRegister))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

// authedRequest creates an HTTP request with the given secret key header.
func authedRequest(t *testing.T, method, url, secretKey string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	require.NoError(t, err)
	req.Header.Set("speakeasy-auth-provider-key", secretKey)
	return req
}

// noFollowClient returns an HTTP client that does not follow redirects.
func noFollowClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// --- Mock mode integration tests ---

func TestMockMode_LoginRedirectsWithCode(t *testing.T) {
	srv := newMockServer(t)

	resp, err := noFollowClient().Get(srv.URL + "/v1/speakeasy_provider/login?return_url=https://example.com/callback&state=abc")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "example.com", loc.Host)
	assert.NotEmpty(t, loc.Query().Get("code"))
	assert.Equal(t, "abc", loc.Query().Get("state"))
}

func TestMockMode_LoginMissingReturnURL(t *testing.T) {
	srv := newMockServer(t)

	resp, err := http.Get(srv.URL + "/v1/speakeasy_provider/login")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMockMode_FullFlow(t *testing.T) {
	srv := newMockServer(t)
	client := srv.Client()

	// Exchange: get a token
	exchangeResp, err := client.Do(authedRequest(t, "POST",
		srv.URL+"/v1/speakeasy_provider/exchange", MockSecretKey,
		strings.NewReader(`{"code":"any-code"}`)))
	require.NoError(t, err)
	defer exchangeResp.Body.Close()
	assert.Equal(t, http.StatusOK, exchangeResp.StatusCode)

	var exchangeBody map[string]string
	require.NoError(t, json.NewDecoder(exchangeResp.Body).Decode(&exchangeBody))
	token := exchangeBody["id_token"]
	require.NotEmpty(t, token)

	// Validate: get user info
	validateReq := authedRequest(t, "GET", srv.URL+"/v1/speakeasy_provider/validate", MockSecretKey, nil)
	validateReq.Header.Set("speakeasy-auth-provider-id-token", token)
	validateResp, err := client.Do(validateReq)
	require.NoError(t, err)
	defer validateResp.Body.Close()
	assert.Equal(t, http.StatusOK, validateResp.StatusCode)

	var vr validateResponse
	require.NoError(t, json.NewDecoder(validateResp.Body).Decode(&vr))
	assert.Equal(t, MockUserEmail, vr.User.Email)
	assert.True(t, vr.User.Admin)
	assert.True(t, vr.User.Whitelisted)
	require.Len(t, vr.Organizations, 1)
	assert.Equal(t, MockOrgName, vr.Organizations[0].Name)
	assert.Equal(t, MockOrgSlug, vr.Organizations[0].Slug)

	// Register: create a new org
	registerReq := authedRequest(t, "POST",
		srv.URL+"/v1/speakeasy_provider/register", MockSecretKey,
		strings.NewReader(`{"organization_name":"New Org","account_type":"pro"}`))
	registerReq.Header.Set("speakeasy-auth-provider-id-token", token)
	registerResp, err := client.Do(registerReq)
	require.NoError(t, err)
	defer registerResp.Body.Close()
	assert.Equal(t, http.StatusOK, registerResp.StatusCode)

	var rr validateResponse
	require.NoError(t, json.NewDecoder(registerResp.Body).Decode(&rr))
	require.Len(t, rr.Organizations, 2)
	assert.Equal(t, "New Org", rr.Organizations[1].Name)
	assert.Equal(t, "new-org", rr.Organizations[1].Slug)
	assert.Equal(t, "pro", rr.Organizations[1].AccountType)

	// Validate again: should include the new org
	validateReq2 := authedRequest(t, "GET", srv.URL+"/v1/speakeasy_provider/validate", MockSecretKey, nil)
	validateReq2.Header.Set("speakeasy-auth-provider-id-token", token)
	validateResp2, err := client.Do(validateReq2)
	require.NoError(t, err)
	defer validateResp2.Body.Close()

	var vr2 validateResponse
	require.NoError(t, json.NewDecoder(validateResp2.Body).Decode(&vr2))
	require.Len(t, vr2.Organizations, 2, "registered org should persist across validate calls")

	// Revoke
	revokeReq := authedRequest(t, "POST", srv.URL+"/v1/speakeasy_provider/revoke", MockSecretKey, nil)
	revokeReq.Header.Set("speakeasy-auth-provider-id-token", token)
	revokeResp, err := client.Do(revokeReq)
	require.NoError(t, err)
	defer revokeResp.Body.Close()
	assert.Equal(t, http.StatusOK, revokeResp.StatusCode)

	// Validate after revoke: should fail
	validateReq3 := authedRequest(t, "GET", srv.URL+"/v1/speakeasy_provider/validate", MockSecretKey, nil)
	validateReq3.Header.Set("speakeasy-auth-provider-id-token", token)
	validateResp3, err := client.Do(validateReq3)
	require.NoError(t, err)
	defer validateResp3.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, validateResp3.StatusCode)
}

func TestMockMode_AuthMiddleware(t *testing.T) {
	srv := newMockServer(t)

	tests := []struct {
		name string
		key  string
	}{
		{"wrong key", "wrong-key"},
		{"empty key", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", srv.URL+"/v1/speakeasy_provider/exchange", strings.NewReader(`{"code":"x"}`))
			require.NoError(t, err)
			if tt.key != "" {
				req.Header.Set("speakeasy-auth-provider-key", tt.key)
			}
			resp, err := srv.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestMockMode_ValidateInvalidToken(t *testing.T) {
	srv := newMockServer(t)

	req := authedRequest(t, "GET", srv.URL+"/v1/speakeasy_provider/validate", MockSecretKey, nil)
	req.Header.Set("speakeasy-auth-provider-id-token", "nonexistent-token")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMockMode_RegisterMissingOrgName(t *testing.T) {
	srv := newMockServer(t)

	// Get a token first
	exchangeResp, err := srv.Client().Do(authedRequest(t, "POST",
		srv.URL+"/v1/speakeasy_provider/exchange", MockSecretKey,
		strings.NewReader(`{"code":"x"}`)))
	require.NoError(t, err)
	var body map[string]string
	json.NewDecoder(exchangeResp.Body).Decode(&body)
	exchangeResp.Body.Close()

	req := authedRequest(t, "POST", srv.URL+"/v1/speakeasy_provider/register", MockSecretKey,
		strings.NewReader(`{"organization_name":""}`))
	req.Header.Set("speakeasy-auth-provider-id-token", body["id_token"])
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- OIDC mode tests ---

func TestOidcMode_LoginRedirectsToWorkOS(t *testing.T) {
	srv, _ := newOidcServer(t)

	resp, err := noFollowClient().Get(srv.URL + "/v1/speakeasy_provider/login?return_url=https://example.com/cb&state=s1")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Contains(t, loc, "https://api.workos.com/user_management/authorize")
	assert.Contains(t, loc, "client_id=client_test")
	assert.Contains(t, loc, "provider=authkit")
	assert.Contains(t, loc, "code_challenge_method=S256")
	assert.Contains(t, loc, "redirect_uri="+url.QueryEscape("http://localhost:35291/v1/speakeasy_provider/oidc/callback"))
}

func TestOidcMode_LoginWithPreviousSessionRedirectsToLogout(t *testing.T) {
	srv, s := newOidcServer(t)

	s.mu.Lock()
	s.lastWorkosSessionID = "sess_prev123"
	s.mu.Unlock()

	resp, err := noFollowClient().Get(srv.URL + "/v1/speakeasy_provider/login?return_url=https://example.com/cb&state=s1")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Contains(t, loc, "https://api.workos.com/user_management/sessions/logout")
	assert.Contains(t, loc, "session_id=sess_prev123")
	assert.Contains(t, loc, url.QueryEscape("http://localhost:35291/v1/speakeasy_provider/logout/callback"))

	// Verify pending logout was stored
	s.mu.Lock()
	pending, ok := s.pendingLogouts["latest"]
	s.mu.Unlock()
	assert.True(t, ok)
	assert.Equal(t, "https://example.com/cb", pending.returnURL)
	assert.Equal(t, "s1", pending.state)
}

func TestOidcMode_LogoutCallbackRestoresParams(t *testing.T) {
	srv, s := newOidcServer(t)

	s.mu.Lock()
	s.pendingLogouts["latest"] = pendingLogout{
		returnURL: "https://example.com/cb",
		state:     "mystate",
	}
	s.mu.Unlock()

	resp, err := noFollowClient().Get(srv.URL + "/v1/speakeasy_provider/logout/callback")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/v1/speakeasy_provider/login", loc.Path)
	assert.Equal(t, "https://example.com/cb", loc.Query().Get("return_url"))
	assert.Equal(t, "mystate", loc.Query().Get("state"))

	// Verify pending logout was consumed
	s.mu.Lock()
	_, ok := s.pendingLogouts["latest"]
	s.mu.Unlock()
	assert.False(t, ok, "pending logout should be consumed")
}

func TestOidcMode_LogoutCallbackNoPending(t *testing.T) {
	srv, _ := newOidcServer(t)

	resp, err := http.Get(srv.URL + "/v1/speakeasy_provider/logout/callback")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestOidcMode_ExchangeAndValidate(t *testing.T) {
	srv, s := newOidcServer(t)

	sess := &oidcSession{
		userSession: &userSession{
			User: user{ID: "u1", Email: "alice@example.com", DisplayName: "Alice", Whitelisted: true},
			Organizations: []organization{
				{ID: "o1", Name: "Alice Org", Slug: "alice-org", AccountType: "free"},
			},
		},
		workosSessionID: "sess_123",
	}
	code := s.createOidcAuthCode(sess)

	// Exchange
	exchangeResp, err := srv.Client().Do(authedRequest(t, "POST",
		srv.URL+"/v1/speakeasy_provider/exchange", "secret",
		strings.NewReader(`{"code":"`+code+`"}`)))
	require.NoError(t, err)
	defer exchangeResp.Body.Close()
	assert.Equal(t, http.StatusOK, exchangeResp.StatusCode)

	var exchangeBody map[string]string
	require.NoError(t, json.NewDecoder(exchangeResp.Body).Decode(&exchangeBody))
	token := exchangeBody["id_token"]
	require.NotEmpty(t, token)

	// Validate
	validateReq := authedRequest(t, "GET", srv.URL+"/v1/speakeasy_provider/validate", "secret", nil)
	validateReq.Header.Set("speakeasy-auth-provider-id-token", token)
	validateResp, err := srv.Client().Do(validateReq)
	require.NoError(t, err)
	defer validateResp.Body.Close()
	assert.Equal(t, http.StatusOK, validateResp.StatusCode)

	var vr validateResponse
	require.NoError(t, json.NewDecoder(validateResp.Body).Decode(&vr))
	assert.Equal(t, "alice@example.com", vr.User.Email)
	assert.Equal(t, "Alice", vr.User.DisplayName)
	require.Len(t, vr.Organizations, 1)
	assert.Equal(t, "Alice Org", vr.Organizations[0].Name)
}

func TestOidcMode_ExchangeInvalidCode(t *testing.T) {
	srv, _ := newOidcServer(t)

	resp, err := srv.Client().Do(authedRequest(t, "POST",
		srv.URL+"/v1/speakeasy_provider/exchange", "secret",
		strings.NewReader(`{"code":"invalid"}`)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestOidcMode_RegisterPersistsOrg(t *testing.T) {
	srv, s := newOidcServer(t)

	sess := &oidcSession{
		userSession: &userSession{
			User:          user{ID: "u1", Email: "bob@example.com", DisplayName: "Bob"},
			Organizations: []organization{{ID: "o1", Name: "Original Org", Slug: "original-org", AccountType: "free"}},
		},
	}
	token := s.createOidcToken(sess)

	// Register a new org
	registerReq := authedRequest(t, "POST", srv.URL+"/v1/speakeasy_provider/register", "secret",
		strings.NewReader(`{"organization_name":"Second Org"}`))
	registerReq.Header.Set("speakeasy-auth-provider-id-token", token)
	registerResp, err := srv.Client().Do(registerReq)
	require.NoError(t, err)
	defer registerResp.Body.Close()
	assert.Equal(t, http.StatusOK, registerResp.StatusCode)

	var rr validateResponse
	require.NoError(t, json.NewDecoder(registerResp.Body).Decode(&rr))
	require.Len(t, rr.Organizations, 2)
	assert.Equal(t, "Second Org", rr.Organizations[1].Name)

	// Validate should also include the new org (persistence test)
	validateReq := authedRequest(t, "GET", srv.URL+"/v1/speakeasy_provider/validate", "secret", nil)
	validateReq.Header.Set("speakeasy-auth-provider-id-token", token)
	validateResp, err := srv.Client().Do(validateReq)
	require.NoError(t, err)
	defer validateResp.Body.Close()

	var vr validateResponse
	require.NoError(t, json.NewDecoder(validateResp.Body).Decode(&vr))
	require.Len(t, vr.Organizations, 2, "registered org should persist to validate calls")
	assert.Equal(t, "Second Org", vr.Organizations[1].Name)
}

func TestOidcMode_RevokeStoresSessionID(t *testing.T) {
	srv, s := newOidcServer(t)

	sess := &oidcSession{
		userSession: &userSession{
			User:          user{ID: "u1", Email: "test@example.com"},
			Organizations: []organization{{ID: "o1", Name: "Test Org", Slug: "test-org"}},
		},
		workosSessionID: "sess_abc",
	}
	token := s.createOidcToken(sess)

	revokeReq := authedRequest(t, "POST", srv.URL+"/v1/speakeasy_provider/revoke", "secret", nil)
	revokeReq.Header.Set("speakeasy-auth-provider-id-token", token)
	resp, err := srv.Client().Do(revokeReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	s.mu.Lock()
	storedID := s.lastWorkosSessionID
	s.mu.Unlock()
	assert.Equal(t, "sess_abc", storedID)
}
