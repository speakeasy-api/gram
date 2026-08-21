package platformmcp

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

type memoryCache struct {
	mu     sync.Mutex
	values map[string]any
}

var _ cache.Cache = (*memoryCache)(nil)

func (c *memoryCache) Get(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.values[key]
	if !ok {
		return errors.New("cache miss")
	}
	challenge, ok := value.(*oauthChallenge)
	if !ok {
		return errors.New("unexpected cache destination")
	}
	cached, ok := entry.(oauthChallenge)
	if !ok {
		return errors.New("unexpected cache value")
	}
	*challenge = cached
	return nil
}

func (c *memoryCache) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.Get(ctx, key, value); err != nil {
		return err
	}
	return c.Delete(ctx, key)
}

func (c *memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
	return nil
}

func (c *memoryCache) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	return true, nil
}
func (c *memoryCache) Update(_ context.Context, key string, value any) error {
	return c.Set(context.Background(), key, value, 0)
}
func (c *memoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, key)
	return nil
}

func (c *memoryCache) Expire(context.Context, string, time.Duration) error          { return nil }
func (c *memoryCache) ListAppend(context.Context, string, any, time.Duration) error { return nil }
func (c *memoryCache) ListRange(context.Context, string, int64, int64, any) error   { return nil }
func (c *memoryCache) DeleteByPrefix(_ context.Context, _ string) error             { return nil }

type testIdentity struct{}

func (testIdentity) BuildAuthorizationURL(_ context.Context, params identity.AuthorizationURLParams) (*url.URL, error) {
	parsed, err := url.Parse("https://idp.example/authorize?state=" + url.QueryEscape(params.State))
	if err != nil {
		return nil, fmt.Errorf("parse test authorization url: %w", err)
	}
	return parsed, nil
}
func (testIdentity) ExchangeCodeForTokens(_ context.Context, _ string) (*identity.IDPUserInfo, error) {
	return &identity.IDPUserInfo{}, nil
}
func (testIdentity) UpsertUserFromIDP(_ context.Context, _ *identity.IDPUserInfo) (string, error) {
	return "user-1", nil
}

type allowGate struct{}

func (allowGate) Enabled(context.Context, string) (bool, error) { return true, nil }

type oauthTestGate struct {
	enabled bool
	err     error
}

func (g oauthTestGate) Enabled(context.Context, string) (bool, error) { return g.enabled, g.err }

type allowAuthorizer struct{}

func (allowAuthorizer) RequireLiveOrgAdmin(context.Context, Principal) error { return nil }

type oauthTestAuthorizer struct {
	err error
}

func (a oauthTestAuthorizer) RequireLiveOrgAdmin(context.Context, Principal) error { return a.err }

type testOrganizationSelector struct {
	organizations []OrganizationOption
}

func (s testOrganizationSelector) EligibleOrganizations(context.Context, string) ([]OrganizationOption, error) {
	return s.organizations, nil
}

func TestOAuthHTTPProviderSetupCompletionDoesNotExposeState(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	response := httptest.NewRecorder()
	service.ProviderSetupCompleteHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/provider-setup-complete?state=secret", nil))

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.NotContains(t, response.Body.String(), "secret")
}

func TestOAuthHTTPMetadataAndClientRegistration(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	metadata := httptest.NewRecorder()
	service.AuthorizationServerHandler().ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp", nil))
	require.Equal(t, http.StatusOK, metadata.Code)
	require.Contains(t, metadata.Body.String(), `"registration_endpoint"`)

	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/register", strings.NewReader(`{"client_name":"test client","redirect_uris":["http://127.0.0.1:3000/callback"],"token_endpoint_auth_method":"none"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	service.RegisterHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)
	require.Contains(t, response.Body.String(), `"client_id"`)
	require.NotContains(t, response.Body.String(), `"client_secret"`)
}

func TestOAuthHTTPRequireJSONAcceptsMediaTypeParameters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		contentType string
		accepted    bool
	}{
		{name: "plain", contentType: "application/json", accepted: true},
		{name: "charset", contentType: "application/json; charset=utf-8", accepted: true},
		{name: "uppercase", contentType: "APPLICATION/JSON", accepted: true},
		{name: "form", contentType: "application/x-www-form-urlencoded", accepted: false},
		{name: "empty", contentType: "", accepted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodPost, "/platform-mcp/register", nil)
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()

			accepted := requireJSON(response, request, 1024)

			require.Equal(t, tc.accepted, accepted)
			if !tc.accepted {
				require.Equal(t, http.StatusBadRequest, response.Code)
				require.Contains(t, response.Body.String(), `"invalid_client_metadata"`)
			}
		})
	}
}

func TestOAuthHTTPRejectsUnknownTokenClient(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token=x&client_id=unknown"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
}

func TestOAuthHTTPSelectsOrganizationAfterIDPCallback(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	service.organizations = testOrganizationSelector{organizations: []OrganizationOption{{ID: "org-1", Name: "Organization one"}}}
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(context.Background(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256", nil))
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/platform-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil))
	selectionURL, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)

	selection := httptest.NewRecorder()
	service.OrganizationSelectionHandler().ServeHTTP(selection, httptest.NewRequest(http.MethodGet, selectionURL.String(), nil))
	require.Equal(t, http.StatusOK, selection.Code)
	require.Contains(t, selection.Body.String(), "Organization one")
	require.Contains(t, selection.Body.String(), "Choose an organization")
	require.Contains(t, selection.Body.String(), "auth-consent-container")
	require.Contains(t, selection.Body.String(), "font-diatype-mono")
	require.NotContains(t, selection.Body.String(), "fonts.googleapis.com")
}

func TestOAuthHTTPRejectsConsentBeforeOrganizationSelection(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: "http://127.0.0.1:3000/callback", CSRFToken: "csrf", Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))

	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/connect?state=challenge-1", nil))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_request"`)
}

func TestOAuthHTTPCompletesChallengeStateHandoff(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(context.Background(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256", nil))
	require.Equal(t, http.StatusFound, authorize.Code)
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/platform-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil))
	require.Equal(t, http.StatusFound, callback.Code)
	selectionURL, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)

	selection := httptest.NewRecorder()
	service.OrganizationSelectionHandler().ServeHTTP(selection, httptest.NewRequest(http.MethodGet, selectionURL.String(), nil))
	require.Equal(t, http.StatusOK, selection.Code)
	state := selectionURL.Query().Get("state")
	require.Contains(t, selection.Body.String(), `name="csrf_token" value="`)
	csrfStart := strings.Index(selection.Body.String(), `name="csrf_token" value="`) + len(`name="csrf_token" value="`)
	csrf := strings.Split(selection.Body.String()[csrfStart:], `"`)[0]

	selected := httptest.NewRecorder()
	selectionForm := url.Values{"state": {state}, "csrf_token": {csrf}, "organization_id": {"org-1"}}
	selectionRequest := httptest.NewRequest(http.MethodPost, "/platform-mcp/select-organization", strings.NewReader(selectionForm.Encode()))
	selectionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	service.OrganizationSelectionHandler().ServeHTTP(selected, selectionRequest)
	require.Equal(t, http.StatusSeeOther, selected.Code)

	connectURL, err := url.Parse(selected.Header().Get("Location"))
	require.NoError(t, err)
	connect := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(connect, httptest.NewRequest(http.MethodGet, connectURL.String(), nil))
	require.Equal(t, http.StatusOK, connect.Code)
	require.Contains(t, connect.Body.String(), "test")
	require.Contains(t, connect.Body.String(), "Organization one")
	require.Contains(t, connect.Body.String(), "data-single-submit")
	contentSecurityPolicy := connect.Header().Get("Content-Security-Policy")
	_, noncePart, found := strings.Cut(contentSecurityPolicy, "script-src 'nonce-")
	require.True(t, found)
	scriptNonce, _, found := strings.Cut(noncePart, "'")
	require.True(t, found)
	require.NotEmpty(t, scriptNonce)
	require.Contains(t, connect.Body.String(), `nonce="`+scriptNonce+`"`)
	require.Contains(t, connect.Body.String(), `action.name = "action"`)
	require.Contains(t, contentSecurityPolicy, "form-action 'self' http://127.0.0.1:3000")

	approve := httptest.NewRecorder()
	approveForm := url.Values{"state": {state}, "csrf_token": {csrf}, "action": {"approve"}}
	approveRequest := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(approveForm.Encode()))
	approveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	service.ConnectHandler().ServeHTTP(approve, approveRequest)
	require.Equal(t, http.StatusSeeOther, approve.Code)
	redirect, err := url.Parse(approve.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:3000/callback", redirect.Scheme+"://"+redirect.Host+redirect.Path)
	require.NotEmpty(t, redirect.Query().Get("code"))

	replayed := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(replayed, approveRequest)
	require.Equal(t, http.StatusUnauthorized, replayed.Code)
}

func TestOAuthPageContentSecurityPolicyOnlyAllowsHTTPRedirectOrigins(t *testing.T) {
	t.Parallel()

	policy := oauthPageContentSecurityPolicy("https://client.example/callback?next=unexpected", "nonce")
	require.Contains(t, policy, "form-action 'self' https://client.example")
	require.NotContains(t, policy, "/callback")
	require.NotContains(t, policy, "?next")

	policy = oauthPageContentSecurityPolicy("javascript:alert(1)", "nonce")
	require.Contains(t, policy, "form-action 'self';")
}

func TestOAuthHTTPGateErrorsDistinguishUnavailableFromDenied(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	principal := Principal{UserID: "user-1", OrganizationID: "org-1", ConnectionID: "connection-1", Generation: "generation-1", ClientID: "client-1"}

	service.gate = oauthTestGate{err: errors.New("feature provider unavailable")}
	err := service.gateAndAuthorize(t.Context(), principal)
	require.ErrorIs(t, err, ErrUnavailable)

	service.gate = oauthTestGate{enabled: true}
	service.authorizer = oauthTestAuthorizer{err: errors.New("authorization store unavailable")}
	err = service.gateAndAuthorize(t.Context(), principal)
	require.ErrorIs(t, err, ErrUnavailable)

	service.authorizer = allowAuthorizer{}
	service.gate = oauthTestGate{enabled: false}
	err = service.gateAndAuthorize(t.Context(), principal)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestOAuthHTTPConnectPostRedirectsTransientGateError(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: "http://127.0.0.1:3000/callback", State: "client-state", CSRFToken: "csrf", OrganizationID: "org-1", Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))
	service.gate = oauthTestGate{err: errors.New("feature provider unavailable")}

	form := url.Values{"state": {challenge.ID}, "csrf_token": {challenge.CSRFToken}, "action": {"approve"}}
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusFound, response.Code)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, challenge.RedirectURI, location.Scheme+"://"+location.Host+location.Path)
	require.Equal(t, challenge.State, location.Query().Get("state"))
	require.Equal(t, "temporarily_unavailable", location.Query().Get("error"))
}

func TestOAuthHTTPRefreshReturnsTransientGateError(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	now := time.Now()
	connection := platformoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "user:user-1", OrganizationID: "org-1", Generation: "generation-1", AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	require.NoError(t, store.RegisterConnection(t.Context(), connection))
	refreshToken, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, store.CreateSession(t.Context(), platformoauth.Session{ID: "session-1", ClientID: "client-1", Connection: connection, JTI: "jti-1", RefreshHash: opaqueHash(refreshToken), ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(time.Hour)}))
	service.gate = oauthTestGate{err: errors.New("feature provider unavailable")}

	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token="+url.QueryEscape(refreshToken)+"&client_id=client-1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), `"temporarily_unavailable"`)
}

func TestOAuthHTTPNeverRedirectsToUnregisteredURI(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?response_type=token&client_id=unknown&redirect_uri=https%3A%2F%2Fevil.example%2F&organization_id=org-1", nil))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Empty(t, response.Header().Get("Location"))
}

func TestOAuthHTTPRefreshReplayIsRejectedBeforeAuthorization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	now := time.Now()
	connection := platformoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "user:user-1", OrganizationID: "org-1", Generation: "generation-1", AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterClient(context.Background(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	require.NoError(t, store.RegisterConnection(context.Background(), connection))
	refreshOld, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID)
	require.NoError(t, err)
	refreshNew, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID)
	require.NoError(t, err)
	old := platformoauth.Session{ID: "session-old", ClientID: "client-1", Connection: connection, JTI: "jti-old", RefreshHash: opaqueHash(refreshOld), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}
	require.NoError(t, store.CreateSession(context.Background(), old))
	replacement := platformoauth.Session{ID: "session-new", ClientID: "client-1", Connection: connection, JTI: "jti-new", RefreshHash: opaqueHash(refreshNew), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}
	_, err = store.RotateSession(context.Background(), platformoauth.RotateSessionInput{OrganizationID: connection.OrganizationID, RefreshHash: old.RefreshHash, ClientID: "client-1", Generation: connection.Generation, Now: now, Replacement: replacement})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token="+url.QueryEscape(refreshOld)+"&client_id=client-1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_grant"`)
	_, err = store.RotateSession(context.Background(), platformoauth.RotateSessionInput{OrganizationID: connection.OrganizationID, RefreshHash: replacement.RefreshHash, ClientID: "client-1", Generation: connection.Generation, Now: now, Replacement: platformoauth.Session{ID: "session-after", ClientID: "client-1", Connection: connection, JTI: "jti-after", RefreshHash: "refresh-after", ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}})
	require.ErrorIs(t, err, platformoauth.ErrRevoked)
}

func TestOAuthHTTPRevokesExpiredAccessToken(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	now := time.Now()
	connection := platformoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "user:user-1", OrganizationID: "org-1", Generation: "generation-1", AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	require.NoError(t, store.RegisterConnection(t.Context(), connection))
	jti, err := service.credentials.Issue(accessJTICredential, connection.OrganizationID)
	require.NoError(t, err)
	accessToken, _, err := service.signer.Mint(sessiontokens.MintParams{Subject: urn.SessionSubject{Kind: urn.SessionSubjectKindUser, ID: "user-1"}, Audience: service.audience, Issuer: service.issuer, Lifetime: -time.Minute, ClientID: "client-1", JTI: jti})
	require.NoError(t, err)
	refreshToken, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, store.CreateSession(t.Context(), platformoauth.Session{ID: "session-1", ClientID: "client-1", Connection: connection, JTI: jti, RefreshHash: opaqueHash(refreshToken), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}))

	form := url.Values{"token": {accessToken}, "token_type_hint": {"access_token"}, "client_id": {"client-1"}}
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/revoke", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.RevokeHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	stored, err := store.GetSessionByRefreshHash(t.Context(), connection.OrganizationID, opaqueHash(refreshToken))
	require.NoError(t, err)
	require.NotNil(t, stored.RevokedAt)
}

func TestOAuthHTTPRejectsMalformedRefreshSubject(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	now := time.Now()
	require.NoError(t, store.RegisterClient(context.Background(), platformoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	connection := platformoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "malformed", OrganizationID: "org-1", Generation: "generation-1", AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterConnection(context.Background(), connection))
	refreshToken, err := service.credentials.Issue(refreshTokenCredential, connection.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, store.CreateSession(context.Background(), platformoauth.Session{ID: "session-1", ClientID: "client-1", Connection: connection, JTI: "jti-1", RefreshHash: opaqueHash(refreshToken), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}))

	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token="+url.QueryEscape(refreshToken)+"&client_id=client-1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_grant"`)
}

func testStore(t *testing.T, service *OAuthHTTP) *platformoauth.InMemoryStore {
	t.Helper()
	store, ok := service.store.(*platformoauth.InMemoryStore)
	require.True(t, ok)
	return store
}

func newTestOAuthHTTP(t *testing.T) *OAuthHTTP {
	t.Helper()
	base, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	service, err := NewOAuthHTTP(OAuthHTTPConfig{
		BaseURL:        base,
		Logger:         testenv.NewLogger(t),
		MeterProvider:  testenv.NewMeterProvider(t),
		Cache:          &memoryCache{values: map[string]any{}},
		Store:          platformoauth.NewInMemoryStore(),
		Identity:       testIdentity{},
		Gate:           allowGate{},
		Authorizer:     allowAuthorizer{},
		Organizations:  testOrganizationSelector{organizations: []OrganizationOption{{ID: "org-1", Name: "Organization one"}}},
		Signer:         sessiontokens.NewSigner("test-key"),
		Encryption:     testEncryption(t),
		GuardianPolicy: guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)),
	})
	require.NoError(t, err)
	return service
}

func testEncryption(t *testing.T) *encryption.Client {
	t.Helper()

	client, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	return client
}

// clientMetadataDocument is the Client ID Metadata Document a test host
// serves. Only the members this authorization server honours are set; the
// resolver requires client_id to equal the URL the document was fetched from.
func clientMetadataDocument(clientID string, redirectURIs []string) string {
	document := map[string]any{
		"client_id":                  clientID,
		"client_name":                "Test Agent",
		"redirect_uris":              redirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// newMetadataDocumentHost serves a Client ID Metadata Document over HTTPS and
// returns its URL plus the number of times the document was fetched. The
// resolver refuses plaintext and never follows redirects, so a TLS server
// whose certificate the guardian policy trusts is the only way to exercise
// the path.
func newMetadataDocumentHost(t *testing.T, redirectURIs []string) (string, *atomic.Int64, *x509.CertPool) {
	t.Helper()

	var fetches atomic.Int64
	var clientID string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(clientMetadataDocument(clientID, redirectURIs)))
	}))
	t.Cleanup(server.Close)
	clientID = server.URL + "/client-metadata"

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return clientID, &fetches, roots
}

// newMetadataOAuthHTTP builds a server that admits any spec-valid document so
// tests can host one locally. Admission itself is covered separately, against
// the production policy.
func newMetadataOAuthHTTP(t *testing.T, roots *x509.CertPool) *OAuthHTTP {
	t.Helper()

	base, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	service, err := NewOAuthHTTP(OAuthHTTPConfig{
		BaseURL:                   base,
		Logger:                    testenv.NewLogger(t),
		MeterProvider:             testenv.NewMeterProvider(t),
		Cache:                     &memoryCache{values: map[string]any{}},
		Store:                     platformoauth.NewInMemoryStore(),
		Identity:                  testIdentity{},
		Gate:                      allowGate{},
		Authorizer:                allowAuthorizer{},
		Organizations:             testOrganizationSelector{organizations: []OrganizationOption{{ID: "org-1", Name: "Organization one"}}},
		Signer:                    sessiontokens.NewSigner("test-key"),
		Encryption:                testEncryption(t),
		GuardianPolicy:            policy,
		TestOnlyCIMDAdmissionMode: admission.ModeOpen,
	})
	require.NoError(t, err)
	return service
}

func authorizeRequest(clientID, redirectURI string) *http.Request {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
	query.Set("code_challenge_method", "S256")
	query.Set("state", "client-state")
	return httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?"+query.Encode(), nil)
}

func TestOAuthHTTPAdvertisesClientIDMetadataDocumentSupport(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	response := httptest.NewRecorder()
	service.AuthorizationServerHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp", nil))

	require.Equal(t, http.StatusOK, response.Code)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &metadata))
	require.Equal(t, true, metadata["client_id_metadata_document_supported"])
	require.Contains(t, metadata["token_endpoint_auth_methods_supported"], "none",
		"a CIMD client is a public client and selects its auth method from this list")
}

func TestOAuthHTTPAuthorizeResolvesClientIDMetadataDocument(t *testing.T) {
	t.Parallel()

	clientID, fetches, roots := newMetadataDocumentHost(t, []string{"http://localhost:4321/callback"})
	service := newMetadataOAuthHTTP(t, roots)

	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, authorizeRequest(clientID, "http://localhost:4321/callback"))

	require.Equal(t, http.StatusFound, response.Code, response.Body.String())
	require.Contains(t, response.Header().Get("Location"), "https://idp.example/authorize")
	require.Equal(t, int64(1), fetches.Load())

	// The document is projected onto a client the rest of the flow can read
	// back by client_id, which is what the consent and token legs do.
	stored, err := service.store.GetClient(t.Context(), clientID)
	require.NoError(t, err)
	require.Equal(t, "Test Agent", stored.Name)
	require.Equal(t, []string{"http://localhost:4321/callback"}, stored.RedirectURIs)
	require.Empty(t, stored.SecretHash, "a metadata document client is always public")
	require.Equal(t, clientID, stored.MetadataURI, "the document URL is the discriminator for a metadata-resolved client")
	require.NotEmpty(t, stored.MetadataCacheExpiresAt, "the freshness window is recorded alongside the projection")
}

func TestOAuthHTTPAuthorizeServesMetadataClientFromCacheWithinFreshness(t *testing.T) {
	t.Parallel()

	clientID, fetches, roots := newMetadataDocumentHost(t, []string{"http://localhost:4321/callback"})
	service := newMetadataOAuthHTTP(t, roots)

	first := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(first, authorizeRequest(clientID, "http://localhost:4321/callback"))
	require.Equal(t, http.StatusFound, first.Code, first.Body.String())

	second := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(second, authorizeRequest(clientID, "http://localhost:4321/callback"))
	require.Equal(t, http.StatusFound, second.Code, second.Body.String())

	require.Equal(t, int64(1), fetches.Load(),
		"a second authorization inside the freshness window must not refetch the document")
}

func TestOAuthHTTPAuthorizeAcceptsVariableLoopbackPortForMetadataClient(t *testing.T) {
	t.Parallel()

	clientID, _, roots := newMetadataDocumentHost(t, []string{"http://localhost:4321/callback"})
	service := newMetadataOAuthHTTP(t, roots)

	first := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(first, authorizeRequest(clientID, "http://localhost:4321/callback"))
	require.Equal(t, http.StatusFound, first.Code, first.Body.String())

	// RFC 8252 §7.3: a native app binds whatever loopback port is free, so a
	// later authorization from the same client arrives on a different one.
	second := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(second, authorizeRequest(clientID, "http://localhost:59122/callback"))
	require.Equal(t, http.StatusFound, second.Code, second.Body.String())
}

func TestOAuthHTTPAuthorizeRejectsUnregisteredRedirectForRegisteredClient(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	require.NoError(t, service.store.RegisterClient(t.Context(), platformoauth.Client{
		ID:              "client-1",
		Name:            "Dynamically registered",
		RedirectURIs:    []string{"http://localhost:4321/callback"},
		SecretHash:      "",
		SecretExpiresAt: nil,
		RevokedAt:       nil,
	}))

	// A dynamically registered client keeps byte-exact matching: its redirect
	// URIs are whatever an unauthenticated caller posted once, not a live
	// statement from an origin the server can re-read.
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, authorizeRequest("client-1", "http://localhost:59122/callback"))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
}

func TestOAuthHTTPAuthorizeDeniesUnlistedMetadataDocument(t *testing.T) {
	t.Parallel()

	clientID, fetches, _ := newMetadataDocumentHost(t, []string{"http://localhost:4321/callback"})
	service := newTestOAuthHTTP(t)

	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, authorizeRequest(clientID, "http://localhost:4321/callback"))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
	require.Contains(t, response.Body.String(), "client policy")
	require.Equal(t, int64(0), fetches.Load(),
		"an unlisted document URL must be denied before any outbound fetch")
}

func TestOAuthHTTPAuthorizeFailsClosedWhenMetadataHostIsUnreachable(t *testing.T) {
	t.Parallel()

	roots := x509.NewCertPool()
	service := newMetadataOAuthHTTP(t, roots)

	// A syntactically valid document URL whose host does not resolve: the
	// spec says a fetch failure aborts the authorization request, and the
	// wire response must stay generic and retryable.
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, authorizeRequest("https://metadata.invalid/client-metadata", "http://localhost:4321/callback"))

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), `"temporarily_unavailable"`)
}
