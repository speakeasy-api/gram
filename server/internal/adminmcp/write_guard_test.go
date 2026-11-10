package adminmcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func writePrincipalContext(t *testing.T, scopes []string) context.Context {
	t.Helper()
	staff := &contextvalues.AdminAuthContext{SessionID: "linked-browser-session", OIDCSubject: staffSubject, Email: "staff@example.test"}
	principal := Principal{
		Subject: "user:" + staffSubject, Email: staff.Email, ClientID: staffClient, ClientRowID: "client-row-1",
		ConnectionID: "connection-1", Generation: "generation-1", Scopes: scopes, staff: staff,
	}
	ctx := context.WithValue(t.Context(), principalKey{}, principal)
	return contextvalues.SetAdminAuthContext(ctx, staff)
}

func TestWriteConfigDefaultsToDisabled(t *testing.T) {
	t.Parallel()
	var config WriteConfig
	require.False(t, config.Enabled)
	_, err := requireWriteAuthority(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), config, OperationSetOrganizationFeature)
	require.ErrorIs(t, err, ErrWriteDisabled)
}

func TestWriteConsentFollowsTheWriteSwitch(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		oauth := &StaffOAuth{writes: WriteConfig{Enabled: enabled}}
		want := []string{ScopeRead}
		if enabled {
			want = append(want, ScopeWrite)
		}
		require.Equal(t, want, oauth.scopesSupported())
		_, allowed := normalizeRequestedScopes(ScopeWrite, oauth.writes.Enabled)
		require.Equal(t, enabled, allowed)
	}
}

func TestRequireWriteAuthority(t *testing.T) {
	t.Parallel()
	enabled := WriteConfig{Enabled: true}

	authority, err := requireWriteAuthority(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), enabled, OperationSetOrganizationFeature)
	require.NoError(t, err)
	require.Equal(t, OperationSetOrganizationFeature, authority.Operation)
	require.Equal(t, "generation-1", authority.Principal.Generation)
	require.Equal(t, staffSubject, authority.Staff.OIDCSubject)

	_, err = requireWriteAuthority(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), WriteConfig{}, OperationSetOrganizationFeature)
	require.ErrorIs(t, err, ErrWriteDisabled)

	_, err = requireWriteAuthority(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), enabled, WriteOperation("delete_global_issuer"))
	require.ErrorIs(t, err, errUnknownWriteOp)

	_, err = requireWriteAuthority(writePrincipalContext(t, []string{ScopeRead}), enabled, OperationSetOrganizationFeature)
	require.ErrorIs(t, err, ErrWriteScope)

	_, err = requireWriteAuthority(t.Context(), enabled, OperationSetOrganizationFeature)
	require.ErrorIs(t, err, ErrWriteIdentity)

	// A principal whose verified staff context was swapped out must not write.
	ctx := writePrincipalContext(t, []string{ScopeRead, ScopeWrite})
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: "other", OIDCSubject: "other", Email: "staff@example.test"})
	_, err = requireWriteAuthority(ctx, enabled, OperationSetOrganizationFeature)
	require.ErrorIs(t, err, ErrWriteIdentity)

	// Missing generation or client row cannot bind a write.
	staff := &contextvalues.AdminAuthContext{SessionID: "s", OIDCSubject: staffSubject, Email: "staff@example.test"}
	partial := Principal{Subject: "user:" + staffSubject, Email: staff.Email, ClientID: staffClient, ConnectionID: "connection-1", Scopes: []string{ScopeRead, ScopeWrite}, staff: staff}
	ctx = contextvalues.SetAdminAuthContext(context.WithValue(t.Context(), principalKey{}, partial), staff)
	_, err = requireWriteAuthority(ctx, enabled, OperationSetOrganizationFeature)
	require.ErrorIs(t, err, ErrWriteIdentity)
}

func TestNormalizeRequestedScopes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw             string
		writesAvailable bool
		want            []string
		ok              bool
	}{
		{raw: "", writesAvailable: false, want: []string{ScopeRead}, ok: true},
		{raw: "admin:read", writesAvailable: true, want: []string{ScopeRead}, ok: true},
		{raw: "admin:write", writesAvailable: true, want: []string{ScopeRead, ScopeWrite}, ok: true},
		{raw: "admin:read admin:write", writesAvailable: true, want: []string{ScopeRead, ScopeWrite}, ok: true},
		{raw: "admin:read admin:write", writesAvailable: false, want: nil, ok: false},
		{raw: "admin:read openid", writesAvailable: true, want: nil, ok: false},
	} {
		got, ok := normalizeRequestedScopes(tt.raw, tt.writesAvailable)
		require.Equal(t, tt.ok, ok, tt.raw)
		require.Equal(t, tt.want, got, tt.raw)
	}
}

func staffConsentWithScope(t *testing.T, writes WriteConfig, scope string) (*recordingStaffAuthorizationStore, *httptest.ResponseRecorder) {
	t.Helper()
	s, store, _, pkce := staffAuthorizationFixture(t)
	s.writes = writes
	request := staffAuthorizeRequest(pkce)
	query := request.URL.Query()
	query.Set("scope", scope)
	request.URL.RawQuery = query.Encode()
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, request)
	if response.Code != http.StatusFound || !strings.Contains(response.Header().Get("Location"), Path+"/connect?state=") {
		return store, response
	}
	connectURL := response.Header().Get("Location")
	proof := staffBrowserProof(t, response)

	get := httptest.NewRequest(http.MethodGet, connectURL, nil)
	get.AddCookie(proof)
	get.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	page := httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(page, get)
	require.Equal(t, http.StatusOK, page.Code)

	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	challenge, err := s.cache.Get(t.Context(), staffChallengePrefix+state)
	require.NoError(t, err)
	form := url.Values{"state": {state}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	post := httptest.NewRequest(http.MethodPost, Path+"/connect", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(proof)
	post.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	result := httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(result, post)
	// Return the consent page body alongside the final response for assertions.
	result.Header().Set("X-Test-Consent-Page", page.Body.String())
	return store, result
}

func TestStaffConsentRechecksWriteSwitchOnSubmit(t *testing.T) {
	t.Parallel()
	s, store, _, pkce := staffAuthorizationFixture(t)
	s.writes = WriteConfig{Enabled: true}
	request := staffAuthorizeRequest(pkce)
	query := request.URL.Query()
	query.Set("scope", "admin:read admin:write")
	request.URL.RawQuery = query.Encode()
	response := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusFound, response.Code)
	connectURL := response.Header().Get("Location")
	proof := staffBrowserProof(t, response)
	get := httptest.NewRequest(http.MethodGet, connectURL, nil)
	get.AddCookie(proof)
	get.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	page := httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(page, get)
	require.Equal(t, http.StatusOK, page.Code)

	parsed, err := url.Parse(connectURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	challenge, err := s.cache.Get(t.Context(), staffChallengePrefix+state)
	require.NoError(t, err)
	s.writes = WriteConfig{}
	form := url.Values{"state": {state}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	post := httptest.NewRequest(http.MethodPost, Path+"/connect", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(proof)
	post.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "browser-session"})
	result := httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(result, post)
	require.Equal(t, http.StatusSeeOther, result.Code)
	callback, err := url.Parse(result.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "invalid_scope", callback.Query().Get("error"))
	require.Zero(t, store.calls)
}

func TestStaffConsentGrantsWriteOnlyWhenSwitchedOn(t *testing.T) {
	t.Parallel()
	enabled := WriteConfig{Enabled: true}

	store, response := staffConsentWithScope(t, enabled, "admin:read admin:write")
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Equal(t, []string{ScopeRead, ScopeWrite}, store.input.Scopes)
	require.Contains(t, response.Header().Get("X-Test-Consent-Page"), "admin:write")

	store, response = staffConsentWithScope(t, enabled, "")
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Equal(t, []string{ScopeRead}, store.input.Scopes)
	require.Contains(t, response.Header().Get("X-Test-Consent-Page"), "read-only")

	// Writes switched off: a write request is refused before any consent page.
	store, response = staffConsentWithScope(t, WriteConfig{}, "admin:read admin:write")
	require.Equal(t, http.StatusSeeOther, response.Code)
	callback, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "invalid_scope", callback.Query().Get("error"))
	require.Zero(t, store.calls)
}
