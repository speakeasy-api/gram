package adminmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recordingStaffAuthorizationStore struct {
	input staffAuthorization
	calls int
	err   error
}

func (s *recordingStaffAuthorizationStore) Authorize(_ context.Context, input staffAuthorization) error {
	s.calls++
	s.input = input
	return s.err
}

func staffAuthorizationFixture(t *testing.T) (*StaffOAuthAuthorization, *recordingStaffAuthorizationStore, *fakeAdminVerifier, string) {
	t.Helper()
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	clients := &recordingStaffClientStore{client: staffOAuthClient{}}
	clients.client = staffOAuthClient{ID: staffClient, Name: "Test editor", RedirectURIs: []string{"http://localhost:5555/callback"}, SecretHash: "", SecretExpiresAt: nil}
	store := &recordingStaffAuthorizationStore{}
	verifier := &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: staffSubject, Email: "staff@example.test"}}
	authorization := NewStaffOAuthAuthorization(clients, store, testenv.NewMemoryCache(), verifier, cipher, staffAudience)
	verifierHash := sha256.Sum256([]byte(strings.Repeat("x", 43)))
	challenge := base64.RawURLEncoding.EncodeToString(verifierHash[:])
	return authorization, store, verifier, challenge
}

func staffBrowserProof(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, staffBrowserProofCookie+"-") {
			require.True(t, cookie.Secure)
			require.True(t, cookie.HttpOnly)
			require.Empty(t, cookie.Domain)
			require.Equal(t, "/", cookie.Path)
			require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
			require.Equal(t, int(staffCodeLifetime.Seconds()), cookie.MaxAge)
			return cookie
		}
	}
	t.Fatal("missing browser proof cookie")
	return nil
}

func staffAuthorizeRequest(challenge string) *http.Request {
	query := url.Values{
		"client_id": {staffClient}, "redirect_uri": {"http://localhost:5555/callback"},
		"response_type": {"code"}, "state": {"client-state"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {staffAudience},
	}
	return httptest.NewRequest(http.MethodGet, Path+"/authorize?"+query.Encode(), nil)
}

func TestStaffOAuthAuthorizationRequiresStaffConsent(t *testing.T) {
	t.Parallel()
	s, store, verifier, pkce := staffAuthorizationFixture(t)
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, staffAuthorizeRequest(pkce))
	require.Equal(t, http.StatusFound, response.Code)
	connectURL := response.Header().Get("Location")
	require.Contains(t, connectURL, Path+"/connect?state=")
	proof := staffBrowserProof(t, response)

	request := httptest.NewRequest(http.MethodGet, connectURL, nil)
	request.AddCookie(proof)
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusFound, response.Code)
	require.Contains(t, response.Header().Get("Location"), "/admin/auth.login?return_to=")
	require.Zero(t, store.calls)

	request = httptest.NewRequest(http.MethodGet, connectURL, nil)
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Test editor")
	require.NotContains(t, response.Body.String(), "browser-session")
	require.Equal(t, "browser-session", verifier.key)
	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	challengeState, err := s.cache.Get(t.Context(), staffChallengePrefix+state)
	require.NoError(t, err)

	form := url.Values{"state": {state}, "csrf_token": {challengeState.CSRFToken}, "action": {"approve"}}
	request = httptest.NewRequest(http.MethodPost, Path+"/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)
	callback, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "localhost:5555", callback.Host)
	require.Equal(t, "client-state", callback.Query().Get("state"))
	require.Equal(t, staffTokenHash(callback.Query().Get("code")), store.input.CodeHash)
	require.Equal(t, staffAudience, store.input.ResourceURI)
	require.Equal(t, []string{"admin:read"}, store.input.Scopes)
	require.NotEqual(t, "browser-session", store.input.SessionEnc)
	require.Equal(t, 1, store.calls)

	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, 1, store.calls)
}

func TestStaffOAuthAuthorizationRejectsCopiedBrowserState(t *testing.T) {
	t.Parallel()
	s, store, verifier, pkce := staffAuthorizationFixture(t)
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, staffAuthorizeRequest(pkce))
	require.Equal(t, http.StatusFound, response.Code)
	connectURL := response.Header().Get("Location")
	proof := staffBrowserProof(t, response)

	for _, cookie := range []*http.Cookie{nil, {Name: proof.Name, Value: "wrong-proof"}} {
		request := httptest.NewRequest(http.MethodGet, connectURL, nil)
		request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response = httptest.NewRecorder()
		s.ConnectHandler().ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
	}
	require.Zero(t, verifier.calls)
	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	challenge, err := s.cache.Get(t.Context(), staffChallengePrefix+parsed.Query().Get("state"))
	require.NoError(t, err)
	form := url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	for _, cookie := range []*http.Cookie{nil, {Name: proof.Name, Value: "wrong-proof"}} {
		request := httptest.NewRequest(http.MethodPost, Path+"/connect", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response = httptest.NewRecorder()
		s.ConnectHandler().ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.NoError(t, s.cache.Store(t.Context(), challenge))
	}
	require.Zero(t, store.calls)
	require.Zero(t, verifier.calls)

	request := httptest.NewRequest(http.MethodGet, connectURL, nil)
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
}

func TestStaffOAuthAuthorizationSupportsConcurrentBrowserChallenges(t *testing.T) {
	t.Parallel()
	s, store, _, pkce := staffAuthorizationFixture(t)
	first := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(first, staffAuthorizeRequest(pkce))
	require.Equal(t, http.StatusFound, first.Code)
	firstProof := staffBrowserProof(t, first)
	second := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(second, staffAuthorizeRequest(pkce))
	require.Equal(t, http.StatusFound, second.Code)
	secondProof := staffBrowserProof(t, second)
	require.NotEqual(t, firstProof.Name, secondProof.Name)

	for _, flow := range []struct {
		url string
	}{
		{url: first.Header().Get("Location")},
		{url: second.Header().Get("Location")},
	} {
		request := httptest.NewRequest(http.MethodGet, flow.url, nil)
		request.AddCookie(firstProof)
		request.AddCookie(secondProof)
		request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
		response := httptest.NewRecorder()
		s.ConnectHandler().ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
	}
	require.Zero(t, store.calls)
}

func TestStaffOAuthAuthorizationRejectsUntrustedRequestAndIdentity(t *testing.T) {
	t.Parallel()
	s, store, verifier, pkce := staffAuthorizationFixture(t)
	request := staffAuthorizeRequest(pkce)
	query := request.URL.Query()
	query.Set("redirect_uri", "https://untrusted.example.test/callback")
	request.URL.RawQuery = query.Encode()
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Empty(t, response.Header().Get("Location"))

	request = staffAuthorizeRequest(pkce)
	query = request.URL.Query()
	query.Set("resource", "https://untrusted.example.test/admin-mcp")
	request.URL.RawQuery = query.Encode()
	response = httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Contains(t, response.Header().Get("Location"), "error=invalid_target")

	response = httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, staffAuthorizeRequest(pkce))
	connectURL := response.Header().Get("Location")
	proof := staffBrowserProof(t, response)
	verifier.result.SessionID = "other-session"
	request = httptest.NewRequest(http.MethodGet, connectURL, nil)
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, store.calls)
}

func TestStaffOAuthAuthorizationRejectsChangedBrowserSession(t *testing.T) {
	t.Parallel()
	s, store, verifier, pkce := staffAuthorizationFixture(t)
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, staffAuthorizeRequest(pkce))
	connectURL := response.Header().Get("Location")
	proof := staffBrowserProof(t, response)
	request := httptest.NewRequest(http.MethodGet, connectURL, nil)
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	challenge, err := s.cache.Get(t.Context(), staffChallengePrefix+parsed.Query().Get("state"))
	require.NoError(t, err)
	form := url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	verifier.result.SessionID = "another-session"
	request = httptest.NewRequest(http.MethodPost, Path+"/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "another-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, store.calls)
}

func TestStaffOAuthAuthorizationMapsRevokedClient(t *testing.T) {
	t.Parallel()
	s, store, _, pkce := staffAuthorizationFixture(t)
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, staffAuthorizeRequest(pkce))
	connectURL := response.Header().Get("Location")
	proof := staffBrowserProof(t, response)
	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	challenge, err := s.cache.Get(t.Context(), staffChallengePrefix+parsed.Query().Get("state"))
	require.NoError(t, err)
	challenge.SessionHash = staffTokenHash("browser-session")
	require.NoError(t, s.cache.Store(t.Context(), challenge))
	store.err = errStaffGrant
	form := url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	request := httptest.NewRequest(http.MethodPost, Path+"/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(proof)
	request.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_client"`)
	require.Equal(t, 1, store.calls)

	store.err = errors.New("database unavailable")
	require.NoError(t, s.cache.Store(t.Context(), challenge))
	response = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, 2, store.calls)
}

func TestStaffAuthorizationStateExpires(t *testing.T) {
	t.Parallel()
	s, store, _, _ := staffAuthorizationFixture(t)
	challenge := staffChallenge{ID: "expired", ClientID: staffClient, RedirectURI: "http://localhost:5555/callback", State: "", CodeChallenge: "", CSRFToken: "csrf", ResourceURI: staffAudience, CreatedAt: time.Now().Add(-time.Hour)}
	require.NoError(t, s.cache.Store(t.Context(), challenge))
	response := httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, Path+"/connect?state=expired", nil))
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, store.calls)
}
