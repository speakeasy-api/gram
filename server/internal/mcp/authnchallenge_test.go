package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// mockIdentityResolver is a test double for the mcp.IdentityResolver interface.
type mockIdentityResolver struct {
	buildAuthURLResult *url.URL
	buildAuthURLErr    error

	exchangeResult *identity.IDPUserInfo
	exchangeErr    error

	upsertResult string
	upsertErr    error

	hasAccessResult *sessions.Organization
	hasAccessEmail  string
	hasAccessOK     bool
}

func (m *mockIdentityResolver) BuildAuthorizationURL(_ context.Context, _ identity.AuthorizationURLParams) (*url.URL, error) {
	return m.buildAuthURLResult, m.buildAuthURLErr
}

func (m *mockIdentityResolver) ExchangeCodeForTokens(_ context.Context, _ string) (*identity.IDPUserInfo, error) {
	return m.exchangeResult, m.exchangeErr
}

func (m *mockIdentityResolver) UpsertUserFromIDP(_ context.Context, _ *identity.IDPUserInfo) (string, error) {
	return m.upsertResult, m.upsertErr
}

func (m *mockIdentityResolver) HasAccessToOrganization(_ context.Context, _, _ string) (*sessions.Organization, string, bool) {
	return m.hasAccessResult, m.hasAccessEmail, m.hasAccessOK
}

// seedPrivateToolsetWithIssuer creates a private toolset backed by a
// user_session_issuer and a registered OAuth client. Returns the toolset,
// issuer, and client rows.
func seedPrivateToolsetWithIssuer(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
) (toolsets_repo.Toolset, usersessions_repo.UserSessionIssuer, usersessions_repo.UserSessionClient) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "private-authn-" + uuid.New().String()[:8]
	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Authn MCP",
		Slug:                   slug,
		Description:            conv.ToPGText("private MCP with authn challenge"),
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               slug + "-issuer",
		AuthnChallengeMode: "chain",
		SessionDuration:    pgtype.Interval{Microseconds: 3600 * 1e6, Valid: true},
	})
	require.NoError(t, err)

	toolset, err = toolsets_repo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)

	client, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuer.ID,
		ClientID:            "test-client-" + uuid.New().String()[:8],
		ClientName:          "test client",
		RedirectUris:        []string{"http://localhost:3000/callback"},
	})
	require.NoError(t, err)

	return toolset, issuer, client
}

func TestHandleAuthorize_PrivateToolset_RedirectsToIDP(t *testing.T) {
	t.Parallel()

	idpURL, _ := url.Parse("https://idp.example.com/authorize?state=challenge123")
	mock := &mockIdentityResolver{
		buildAuthURLResult: idpURL,
	}

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {client.RedirectUris[0]},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleAuthorize(w, req)
	require.NoError(t, err)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "idp.example.com/authorize", "should redirect to IDP")
}

func TestHandleAuthorize_PublicToolset_RedirectsToConsent(t *testing.T) {
	t.Parallel()

	mock := &mockIdentityResolver{}

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	// Make it public
	toolset, err := toolsets_repo.New(ti.conn).UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {client.RedirectUris[0]},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleAuthorize(w, req)
	require.NoError(t, err)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "/connect", "public toolset should redirect to consent page")
	require.Contains(t, loc, "state=", "consent redirect should carry challenge state")

	parsedLoc, err := url.Parse(loc)
	require.NoError(t, err)
	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+parsedLoc.Query().Get("state"))
	require.NoError(t, err)
	require.NotEmpty(t, stored.CSRFToken)
	require.NotNil(t, stored.Subject)
	require.Equal(t, urn.SessionSubjectKindAnonymous, stored.Subject.Kind)
}

func TestHandleAuthorize_InvalidClientID_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {"bogus-client"},
		"redirect_uri":          {"http://localhost:3000/callback"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleAuthorize(w, req)
	require.NoError(t, err) // error written to response body, not returned

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "invalid_client", body["error"])
}

func TestHandleConsentGet_RendersCSRFToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	subject := urn.NewUserSubject("consent-user-" + uuid.NewString())
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()

	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           csrfToken,
		Subject:             &subject,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/connect?state="+stateID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleConsent(w, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `name="csrf_token" value="`+csrfToken+`"`)
}

func TestHandleConsentPost_RejectsInvalidCSRFToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	subject := urn.NewUserSubject("consent-user-" + uuid.NewString())
	stateID := uuid.NewString()

	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "expected-csrf",
		Subject:             &subject,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", "wrong-csrf")
	form.Set("action", "approve")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleConsent(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid consent csrf token")
}

func TestHandleConsentPost_ApproveWithCSRFRedirectsWithCode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	subject := urn.NewUserSubject("consent-user-" + uuid.NewString())
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()

	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           csrfToken,
		Subject:             &subject,
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleConsent(w, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "client-state", loc.Query().Get("state"))
	require.NotEmpty(t, loc.Query().Get("code"))
}

func TestHandleIDPCallback_ExchangesCodeAndRedirectsToConsent(t *testing.T) {
	t.Parallel()

	gramUserID := "user-" + uuid.New().String()[:8]
	mock := &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{
			Sub:   "workos-user-123",
			Email: "test@example.com",
			Name:  "Test User",
		},
		upsertResult: gramUserID,
		hasAccessResult: &sessions.Organization{
			ID:   "org-id-placeholder",
			Name: "Test Org",
		},
		hasAccessEmail: "test@example.com",
		hasAccessOK:    true,
	}

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	// Seed a challenge state in Redis (simulating HandleAuthorize having run)
	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		State:               "client-state",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"state": {challengeID},
		"code":  {"idp-auth-code-123"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(w, req)
	require.NoError(t, err)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "/connect", "should redirect to consent page")
	require.Contains(t, loc, "state=", "consent redirect should carry new challenge state")
	// The state in the redirect should NOT be the original challengeID (it gets rotated)
	require.NotContains(t, loc, challengeID, "challenge state should be rotated after IDP callback")
}

func TestHandleIDPCallback_UserNotInOrg_ReturnsForbidden(t *testing.T) {
	t.Parallel()

	mock := &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{
			Sub:   "workos-user-456",
			Email: "outsider@example.com",
			Name:  "Outsider",
		},
		upsertResult: "user-outsider",
		hasAccessOK:  false, // user NOT in the org
	}

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"state": {challengeID},
		"code":  {"idp-auth-code-456"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a member")
}

func TestHandleIDPCallback_MissingState_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	mcpSlug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?code=abc", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleIDPCallback(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "state is required")
}

func TestHandleIDPCallback_IDPError_ForwardsToClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"state":             {challengeID},
		"error":             {"access_denied"},
		"error_description": {"user cancelled"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(w, req)
	require.NoError(t, err)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "error=access_denied", "should forward IDP error to client redirect")
	require.Contains(t, loc, "localhost:3000/callback", "should redirect to client's redirect_uri")
}

func TestHandleIDPCallback_ExpiredState_ReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	// Use a random state ID that was never stored — simulates expiry.
	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"state": {uuid.NewString()},
		"code":  {"some-code"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleIDPCallback(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found or expired")
}

func TestHandleIDPCallback_ToolsetMismatch_ReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	// Seed challenge state with a different toolset ID than the one in the URL.
	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        "wrong-toolset-slug",
			CustomDomainID: uuid.NullUUID{Valid: false},
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"state": {challengeID},
		"code":  {"some-code"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match")
}

func TestHandleIDPCallback_MissingCode_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	// state present but no code and no error — should fail
	q := url.Values{
		"state": {challengeID},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "code is required")
}

func TestHandleIDPCallback_ExchangeFailure_ReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	mock := &mockIdentityResolver{
		exchangeErr: fmt.Errorf("IDP token exchange failed"),
	}

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
		},
		ClientID:            "test-client",
		RedirectURI:         "http://localhost:3000/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	})
	require.NoError(t, err)

	mcpSlug := toolset.McpSlug.String
	q := url.Values{
		"state": {challengeID},
		"code":  {"bad-code"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err = ti.service.HandleIDPCallback(w, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to exchange IDP code")
}
