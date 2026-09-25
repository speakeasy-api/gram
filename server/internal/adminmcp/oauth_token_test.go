package adminmcp

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

type recordingStaffGrantStore struct {
	connection staffTokenConnection
	issued     staffIssuedSession
	validate   int
	exchange   int
	prepare    int
	rotate     int
	status     error
}

func (s *recordingStaffGrantStore) ValidateGrant(_ context.Context, _, _, _, _ string, _ time.Time) (staffTokenConnection, error) {
	s.validate++
	return s.connection, s.status
}
func (s *recordingStaffGrantStore) ExchangeGrant(_ context.Context, _, _, _, _ string, session staffIssuedSession, _ time.Time) error {
	s.exchange++
	s.issued = session
	return s.status
}
func (s *recordingStaffGrantStore) PrepareRefresh(_ context.Context, _, _ string, _ time.Time) (staffTokenConnection, error) {
	s.prepare++
	return s.connection, s.status
}
func (s *recordingStaffGrantStore) RotateRefresh(_ context.Context, _, _ string, _ staffTokenConnection, session staffIssuedSession, _ time.Time) error {
	s.rotate++
	s.issued = session
	return s.status
}

func staffTokensFixture(t *testing.T) (*StaffOAuthTokens, *recordingStaffGrantStore, *fakeAdminVerifier) {
	t.Helper()
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	enc, err := cipher.Encrypt([]byte("linked-browser-session"))
	require.NoError(t, err)
	secretHash, err := bcrypt.GenerateFromPassword([]byte("test-client-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	client := &recordingStaffClientStore{client: staffOAuthClient{ID: staffClient, Name: "Test editor", SecretHash: string(secretHash), RedirectURIs: []string{"http://localhost:5555/callback"}, SecretExpiresAt: nil}}
	store := &recordingStaffGrantStore{connection: staffTokenConnection{ID: uuid.New(), ClientRowID: uuid.New(), Subject: "user:" + staffSubject, SessionEnc: enc, ResourceURI: staffAudience, Scopes: []string{"admin:read"}, Generation: uuid.New(), AuthorizedTil: time.Now().Add(time.Hour)}}
	verifier := &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{SessionID: "linked-browser-session", OIDCSubject: staffSubject, Email: "staff@example.test"}}
	tokens := NewStaffOAuthTokens(client, store, verifier, cipher, sessiontokens.NewSigner("staff-test-signing-key"), staffIssuer, staffAudience)
	return tokens, store, verifier
}

func staffTokenRequest(grantType string, extras url.Values) *http.Request {
	form := url.Values{"grant_type": {grantType}, "resource": {staffAudience}}
	maps.Copy(form, extras)
	request := httptest.NewRequest(http.MethodPost, Path+"/token", strings.NewReader(form.Encode()))
	request.SetBasicAuth(staffClient, "test-client-secret")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func TestStaffOAuthCodeExchangeRequiresLiveStaff(t *testing.T) {
	t.Parallel()
	tokens, store, verifier := staffTokensFixture(t)
	request := staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}})
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "Bearer", body["token_type"])
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	refresh, ok := body["refresh_token"].(string)
	require.True(t, ok)
	require.NotEmpty(t, refresh)
	require.Equal(t, staffTokenHash(refresh), store.issued.RefreshHash)
	require.NotEqual(t, store.issued.RefreshHash, refresh)
	require.Equal(t, "linked-browser-session", verifier.key)
	require.Equal(t, 1, store.validate)
	require.Equal(t, 1, store.exchange)
	access, ok := body["access_token"].(string)
	require.True(t, ok)
	claims, err := tokens.signer.ValidateExactAudience(access, staffAudience)
	require.NoError(t, err)
	require.Equal(t, staffIssuer, claims.Issuer)
	require.Equal(t, store.issued.JTI, claims.ID)
	require.Equal(t, staffClient, claims.ClientID)
}

func TestStaffOAuthRefreshAndInvalidGrant(t *testing.T) {
	t.Parallel()
	tokens, store, verifier := staffTokensFixture(t)
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, staffTokenRequest("refresh_token", url.Values{"refresh_token": {"old-refresh-token"}}))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, store.prepare)
	require.Equal(t, 1, store.rotate)
	require.Equal(t, "linked-browser-session", verifier.key)

	store.status = errStaffRefreshReuse
	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, staffTokenRequest("refresh_token", url.Values{"refresh_token": {"used-token"}}))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_grant"`)
	require.NotContains(t, response.Body.String(), "used-token")
}

func TestStaffOAuthTokenRetainsGrantOnVerifierOutage(t *testing.T) {
	t.Parallel()
	tokens, store, verifier := staffTokensFixture(t)
	verifier.err = oops.C(oops.CodeUnexpected)
	request := staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}})
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), `"error":"temporarily_unavailable"`)
	require.Zero(t, store.exchange)
	verifier.err = oops.C(oops.CodeUnauthorized)
	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_grant"`)
	require.Zero(t, store.exchange)
}

func TestStaffOAuthTokenRejectsCookiesAndForeignResource(t *testing.T) {
	t.Parallel()
	tokens, store, verifier := staffTokensFixture(t)
	request := staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}})
	request.AddCookie(&http.Cookie{Name: "gram_admin", Value: "not-the-linked-session"})
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "linked-browser-session", verifier.key)
	require.Equal(t, 1, store.exchange)

	request = staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}, "resource": {staffAudience, "https://other.example.test/admin-mcp"}})
	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, 1, store.validate)
	require.Contains(t, response.Body.String(), `"error":"invalid_target"`)
}

func TestStaffOAuthTokenRejectsChangedStaffSession(t *testing.T) {
	t.Parallel()
	tokens, store, verifier := staffTokensFixture(t)
	verifier.result.SessionID = "not-the-linked-session"
	request := staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}})
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Zero(t, store.exchange)
}

func publicStaffTokenRequest(grantType string, extras url.Values) *http.Request {
	form := url.Values{"grant_type": {grantType}, "resource": {staffAudience}, "client_id": {staffClient}}
	maps.Copy(form, extras)
	request := httptest.NewRequest(http.MethodPost, Path+"/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func usePublicStaffClient(t *testing.T, tokens *StaffOAuthTokens) {
	t.Helper()
	clients, ok := tokens.clients.(*recordingStaffClientStore)
	require.True(t, ok)
	clients.client.SecretHash = ""
}

func TestStaffOAuthPublicClientExchangeAndRefresh(t *testing.T) {
	t.Parallel()
	tokens, store, verifier := staffTokensFixture(t)
	usePublicStaffClient(t, tokens)

	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, publicStaffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}}))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, store.exchange)
	require.Equal(t, "linked-browser-session", verifier.key)

	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, publicStaffTokenRequest("refresh_token", url.Values{"refresh_token": {"old-refresh-token"}}))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, store.rotate)

	store.status = errStaffRefreshReuse
	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, publicStaffTokenRequest("refresh_token", url.Values{"refresh_token": {"used-token"}}))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_grant"`)
}

func TestStaffOAuthPublicClientCannotPresentCredentials(t *testing.T) {
	t.Parallel()
	tokens, store, _ := staffTokensFixture(t)
	usePublicStaffClient(t, tokens)

	// Basic auth, even with an empty secret, is the confidential presentation.
	form := url.Values{"grant_type": {"authorization_code"}, "resource": {staffAudience}, "code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}}
	request := httptest.NewRequest(http.MethodPost, Path+"/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(staffClient, "")
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)

	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, publicStaffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "client_secret": {"guess"}}))
	require.Equal(t, http.StatusUnauthorized, response.Code)

	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, publicStaffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "client_id": {staffClient, "client_other"}}))
	require.Equal(t, http.StatusUnauthorized, response.Code)

	// Any other Authorization header is an ambiguous presentation, not absent.
	for _, header := range []string{"Bearer some-token", "Basic not-base64", "Basic"} {
		request := publicStaffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}})
		request.Header.Set("Authorization", header)
		response = httptest.NewRecorder()
		tokens.TokenHandler().ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code, header)
	}
	require.Zero(t, store.validate)
}

func TestStaffOAuthConfidentialClientCannotDowngradeToPublic(t *testing.T) {
	t.Parallel()
	tokens, store, _ := staffTokensFixture(t)
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, publicStaffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "redirect_uri": {"http://localhost:5555/callback"}, "code_verifier": {strings.Repeat("x", 43)}}))
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"error":"invalid_client"`)
	require.Zero(t, store.validate)
}

func TestStaffOAuthTokenRejectsPublicAndBodyClientCredentials(t *testing.T) {
	t.Parallel()
	tokens, store, _ := staffTokensFixture(t)
	request := staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}})
	request.Header.Del("Authorization")
	response := httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)

	request = staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "client_id": {staffClient}})
	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, store.validate)

	request = staffTokenRequest("authorization_code", url.Values{"code": {"one-time-code"}, "client_secret": {"test-client-secret"}})
	response = httptest.NewRecorder()
	tokens.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, store.validate)
}
