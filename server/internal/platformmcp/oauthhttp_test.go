package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adminoauth "github.com/speakeasy-api/gram/server/internal/adminmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
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

type allowAuthorizer struct{}

func (allowAuthorizer) RequireLiveOrgAdmin(context.Context, Principal) error { return nil }

type testOrganizationSelector struct {
	organizations []OrganizationOption
}

func (s testOrganizationSelector) EligibleOrganizations(context.Context, string) ([]OrganizationOption, error) {
	return s.organizations, nil
}

func TestOAuthHTTPMetadataAndClientRegistration(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	metadata := httptest.NewRecorder()
	service.AuthorizationServerHandler().ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/admin-mcp", nil))
	require.Equal(t, http.StatusOK, metadata.Code)
	require.Contains(t, metadata.Body.String(), `"registration_endpoint"`)

	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/register", strings.NewReader(`{"client_name":"test client","redirect_uris":["http://127.0.0.1:3000/callback"],"token_endpoint_auth_method":"none"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	service.RegisterHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)
	require.Contains(t, response.Body.String(), `"client_id"`)
	require.NotContains(t, response.Body.String(), `"client_secret"`)
}

func TestOAuthHTTPRejectsUnknownTokenClient(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token=x&client_id=unknown"))
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
	require.NoError(t, store.RegisterClient(context.Background(), adminoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/admin-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256", nil))
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/admin-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil))
	selectionURL, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)

	selection := httptest.NewRecorder()
	service.OrganizationSelectionHandler().ServeHTTP(selection, httptest.NewRequest(http.MethodGet, selectionURL.String(), nil))
	require.Equal(t, http.StatusOK, selection.Code)
	require.Contains(t, selection.Body.String(), "Organization one")
}

func TestOAuthHTTPRejectsConsentBeforeOrganizationSelection(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	challenge := oauthChallenge{ID: "challenge-1", ClientID: "client-1", RedirectURI: "http://127.0.0.1:3000/callback", CSRFToken: "csrf", Subject: "user:user-1", CreatedAt: time.Now()}
	require.NoError(t, service.cache.Store(t.Context(), challenge))

	response := httptest.NewRecorder()
	service.ConnectHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin-mcp/connect?state=challenge-1", nil))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_request"`)
}

func TestOAuthHTTPCompletesChallengeStateHandoff(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	require.NoError(t, store.RegisterClient(context.Background(), adminoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))

	authorize := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, "/admin-mcp/authorize?response_type=code&client_id=client-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback&code_challenge=challenge&code_challenge_method=S256", nil))
	require.Equal(t, http.StatusFound, authorize.Code)
	idpURL, err := url.Parse(authorize.Header().Get("Location"))
	require.NoError(t, err)

	callback := httptest.NewRecorder()
	service.IDPCallbackHandler().ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/admin-mcp/idp_callback?state="+url.QueryEscape(idpURL.Query().Get("state"))+"&code=idp-code", nil))
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
	selectionRequest := httptest.NewRequest(http.MethodPost, "/admin-mcp/select-organization", strings.NewReader(selectionForm.Encode()))
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
}

func TestOAuthHTTPNeverRedirectsToUnregisteredURI(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin-mcp/authorize?response_type=token&client_id=unknown&redirect_uri=https%3A%2F%2Fevil.example%2F&organization_id=org-1", nil))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Empty(t, response.Header().Get("Location"))
}

func TestOAuthHTTPRefreshReplayIsRejectedBeforeAuthorization(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	now := time.Now()
	connection := adminoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "user:user-1", OrganizationID: "org-1", Generation: "generation-1"}
	require.NoError(t, store.RegisterClient(context.Background(), adminoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	require.NoError(t, store.RegisterConnection(context.Background(), connection))
	old := adminoauth.Session{ID: "session-old", ClientID: "client-1", Connection: connection, JTI: "jti-old", RefreshHash: opaqueHash("refresh-old"), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}
	require.NoError(t, store.CreateSession(context.Background(), old))
	replacement := adminoauth.Session{ID: "session-new", ClientID: "client-1", Connection: connection, JTI: "jti-new", RefreshHash: opaqueHash("refresh-new"), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}
	_, err := store.RotateSession(context.Background(), adminoauth.RotateSessionInput{RefreshHash: old.RefreshHash, ClientID: "client-1", Generation: connection.Generation, Now: now, Replacement: replacement})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token=refresh-old&client_id=client-1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_grant"`)
	_, err = store.RotateSession(context.Background(), adminoauth.RotateSessionInput{RefreshHash: replacement.RefreshHash, ClientID: "client-1", Generation: connection.Generation, Now: now, Replacement: adminoauth.Session{ID: "session-after", ClientID: "client-1", Connection: connection, JTI: "jti-after", RefreshHash: "refresh-after", ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}})
	require.ErrorIs(t, err, adminoauth.ErrAlreadyUsed)
}

func TestOAuthHTTPRejectsMalformedRefreshSubject(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	store := testStore(t, service)
	now := time.Now()
	require.NoError(t, store.RegisterClient(context.Background(), adminoauth.Client{ID: "client-1", Name: "test", RedirectURIs: []string{"http://127.0.0.1:3000/callback"}}))
	require.NoError(t, store.RegisterConnection(context.Background(), adminoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "malformed", OrganizationID: "org-1", Generation: "generation-1"}))
	require.NoError(t, store.CreateSession(context.Background(), adminoauth.Session{ID: "session-1", ClientID: "client-1", Connection: adminoauth.Connection{ID: "connection-1", ClientID: "client-1", Subject: "malformed", OrganizationID: "org-1", Generation: "generation-1"}, JTI: "jti-1", RefreshHash: opaqueHash("refresh-token"), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}))

	request := httptest.NewRequest(http.MethodPost, "/admin-mcp/token", strings.NewReader("grant_type=refresh_token&refresh_token=refresh-token&client_id=client-1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_grant"`)
}

func testStore(t *testing.T, service *OAuthHTTP) *adminoauth.InMemoryStore {
	t.Helper()
	store, ok := service.store.(*adminoauth.InMemoryStore)
	require.True(t, ok)
	return store
}

func newTestOAuthHTTP(t *testing.T) *OAuthHTTP {
	t.Helper()
	base, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	service, err := NewOAuthHTTP(OAuthHTTPConfig{
		BaseURL:       base,
		Cache:         &memoryCache{values: map[string]any{}},
		Store:         adminoauth.NewInMemoryStore(),
		Identity:      testIdentity{},
		Gate:          allowGate{},
		Authorizer:    allowAuthorizer{},
		Organizations: testOrganizationSelector{organizations: []OrganizationOption{{ID: "org-1", Name: "Organization one"}}},
		Signer:        sessiontokens.NewSigner("test-key"),
	})
	require.NoError(t, err)
	return service
}
