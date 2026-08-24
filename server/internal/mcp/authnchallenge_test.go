package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

// fetchAdvertisedIssuer returns the `issuer` from the AS metadata document as
// served, plus the RFC 9207 support flag.
//
// Clients compare the `iss` on an authorization response to this value with no
// normalization of their own — no case folding, default-port elision,
// trailing-slash or percent-encoding fixups — so the two must match byte for
// byte. Tests assert against the served document rather than a recomputed
// literal, which is what makes them fail when both sides are derived wrong in
// the same way.
func fetchAdvertisedIssuer(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug string) (string, any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleGetAuthorizationServer(w, req))
	require.Equal(t, http.StatusOK, w.Code)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))

	issuer, ok := meta["issuer"].(string)
	require.True(t, ok, "metadata must carry a string issuer: %s", w.Body.String())
	return issuer, meta["authorization_response_iss_parameter_supported"]
}

type consentPostOpts struct {
	mcpSlug        string
	issuerID       uuid.UUID
	clientID       string
	redirectURI    string
	baseURL        string
	customDomainID uuid.NullUUID
	action         string
}

// postConsent seeds an approved-subject challenge and drives the consent POST,
// returning the parsed client redirect. requestCtx is the context the POST is
// served under; opts.baseURL is the mint-time origin snapshot, deliberately
// independent of it so tests can exercise the cross-origin resume.
func postConsent(t *testing.T, ctx context.Context, requestCtx context.Context, ti *testInstance, opts consentPostOpts) *url.URL {
	t.Helper()

	subject := urn.NewUserSubject("consent-user-" + uuid.NewString())
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()

	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: opts.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        opts.mcpSlug,
			CustomDomainID: opts.customDomainID,
			BaseURL:        opts.baseURL,
		},
		ClientID:            opts.clientID,
		RedirectURI:         opts.redirectURI,
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           csrfToken,
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", opts.action)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+opts.mcpSlug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", opts.mcpSlug)
	req = req.WithContext(context.WithValue(requestCtx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return loc
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

func TestHandleAuthorize_PublicToolset_RequireUserIdentity_RedirectsToIDP(t *testing.T) {
	t.Parallel()

	idpURL, _ := url.Parse("https://idp.example.com/authorize?state=challenge123")
	mock := &mockIdentityResolver{
		buildAuthURLResult: idpURL,
	}

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

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
		"requireUserIdentity":   {"1"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))

	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "idp.example.com/authorize",
		"requireUserIdentity should divert public toolset through IDP")
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
		Endpoint: mcp.EndpointRef{
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
		Endpoint: mcp.EndpointRef{
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

// hydrateConsentInventory performs the tools/list call the consent island's
// MCP session runs on page load, returning the attempt id the approve form
// must submit as tool_inventory_id: approval binds to the completed
// snapshot this hydration captures.
func hydrateConsentInventory(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, stateID, csrfToken string) string {
	t.Helper()

	attempt := uuid.NewString()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/connect/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Consent-State", stateID)
	req.Header.Set("Gram-Consent-Csrf", csrfToken)
	req.Header.Set("Gram-Consent-Inventory-Attempt", attempt)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsentMCP(w, req))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"tools"`)
	return attempt
}

func TestHandleConsentPost_ApproveWithCSRFRedirectsWithCode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, toolset.ID.String())
	subject := urn.NewUserSubject(mockidp.MockUserID)
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()

	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
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

	attempt := hydrateConsentInventory(t, ctx, ti, toolset.McpSlug.String, stateID, csrfToken)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	form.Set("tool_inventory_id", attempt)
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

// TestHandleConsentPost_PropagatesFlowIDIntoGrant asserts the flow id is
// carried from the AuthnChallengeState into the UserSessionGrant minted on
// consent approval, so the terminal /token leg (which only reads the grant)
// can still log the same flow_id.
func TestHandleConsentPost_PropagatesFlowIDIntoGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, toolset.ID.String())
	subject := urn.NewUserSubject(mockidp.MockUserID)
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()
	flowID := "flow-" + uuid.NewString()

	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		FlowID:              flowID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
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
	}))

	attempt := hydrateConsentInventory(t, ctx, ti, toolset.McpSlug.String, stateID, csrfToken)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	form.Set("tool_inventory_id", attempt)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "userSessionGrant:"+toolset.UserSessionIssuerID.UUID.String()+":"+code)
	require.NoError(t, err)
	require.Equal(t, flowID, grant.FlowID, "flow id must propagate into the grant")
}

// The flagship invariant: the code-carrying success response is what a client
// validates on the critical path, and its `iss` must equal the advertised
// issuer exactly.
func TestHandleConsentPost_ApproveEmitsIssMatchingAdvertisedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedIssuer, supported := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)
	require.Equal(t, true, supported)

	loc := postConsent(t, ctx, ctx, ti, consentPostOpts{
		mcpSlug:     mcpSlug,
		issuerID:    toolset.UserSessionIssuerID.UUID,
		clientID:    client.ClientID,
		redirectURI: client.RedirectUris[0],
		baseURL:     ti.serverURL.String(),
		action:      "approve",
	})

	require.NotEmpty(t, loc.Query().Get("code"), "approve must still mint a code")
	require.Equal(t, advertisedIssuer, loc.Query().Get("iss"))
}

// RFC 9207 §2 covers error responses too: a user declining consent still gets
// a response the client must be able to attribute to this issuer.
func TestHandleConsentPost_DenyEmitsIss(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	loc := postConsent(t, ctx, ctx, ti, consentPostOpts{
		mcpSlug:     mcpSlug,
		issuerID:    toolset.UserSessionIssuerID.UUID,
		clientID:    client.ClientID,
		redirectURI: client.RedirectUris[0],
		baseURL:     ti.serverURL.String(),
		action:      "deny",
	})

	require.Equal(t, "access_denied", loc.Query().Get("error"))
	require.Equal(t, advertisedIssuer, loc.Query().Get("iss"))
	require.Empty(t, loc.Query().Get("code"))
}

// A challenge minted under a custom domain can be resumed on the platform
// origin: the remote-session return leg bounces through the server URL, so the
// consent POST's own custom-domain context (here, absent) is not the origin the
// client recorded. `iss` must come from the mint-time snapshot regardless.
func TestHandleConsentPost_IssUsesMintOriginNotRequestOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	const mintOrigin = "https://mcp.customer.example"

	// Deliberately no customdomains context on the request: this is the
	// platform-origin re-entry shape.
	loc := postConsent(t, ctx, ctx, ti, consentPostOpts{
		mcpSlug:     mcpSlug,
		issuerID:    toolset.UserSessionIssuerID.UUID,
		clientID:    client.ClientID,
		redirectURI: client.RedirectUris[0],
		baseURL:     mintOrigin,
		action:      "approve",
	})

	require.Equal(t, mintOrigin+"/mcp/"+mcpSlug, loc.Query().Get("iss"),
		"iss must be rebuilt from the mint-time origin, not from the resuming request")
	require.NotContains(t, loc.Query().Get("iss"), ti.serverURL.Host,
		"falling back to the server default origin here silently breaks the client's comparison")
}

// A custom-domain flow that stays on the custom domain end to end: the
// advertised issuer and the emitted iss must both be the custom-domain origin.
func TestHandleConsentPost_CustomDomainIssMatchesAdvertisedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "iss-cd-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	toolset, domain := attachCustomDomainToToolset(t, ctx, ti, authCtx, toolset, "iss-cd.example.com")

	clientID := "iss-custom-domain-client"
	clientRedirectURI := "http://localhost:3000/callback"
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	advertisedIssuer, supported := fetchAdvertisedIssuer(t, domainCtx, ti, slug)
	require.Equal(t, true, supported)
	require.Contains(t, advertisedIssuer, domain.Domain, "sanity: the custom domain must drive the advertised issuer")

	loc := postConsent(t, ctx, domainCtx, ti, consentPostOpts{
		mcpSlug:        slug,
		issuerID:       issuer.ID,
		clientID:       clientID,
		redirectURI:    clientRedirectURI,
		baseURL:        "https://" + domain.Domain,
		customDomainID: toolset.CustomDomainID,
		action:         "approve",
	})

	require.Equal(t, advertisedIssuer, loc.Query().Get("iss"))
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
		Endpoint: mcp.EndpointRef{
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

// TestHandleIDPCallback_PreservesFlowIDAcrossRotation asserts the stable
// flow correlation id survives the deliberate cache-key (ID) rotation in
// HandleIDPCallback. Without preservation, the private-toolset leg of a flow
// would be uncorrelatable from the rest (AC: one flow_id reconstructs the
// whole chain).
func TestHandleIDPCallback_PreservesFlowIDAcrossRotation(t *testing.T) {
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

	flowID := "flow-" + uuid.NewString()
	challengeID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		FlowID:              flowID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
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
	}))

	mcpSlug := toolset.McpSlug.String
	q := url.Values{"state": {challengeID}, "code": {"idp-auth-code-123"}}
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/idp_callback?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	rotatedID := loc.Query().Get("state")
	require.NotEmpty(t, rotatedID)
	require.NotEqual(t, challengeID, rotatedID, "cache-key id must rotate")

	rotated, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+rotatedID)
	require.NoError(t, err)
	require.Equal(t, flowID, rotated.FlowID, "flow id must survive the rotation")
}

// TestHandleIDPCallback_UsesBaseURLFromCachedState verifies that the
// consent-redirect Location is built from the BaseURL snapshotted onto
// the cached EndpointRef at mint time, rather than from s.serverURL.
// The IDP callback is registered at a global URL and loses the
// originating request's customdomains.Context, so without this
// snapshot a challenge minted under a custom domain would be resumed
// on the server default origin — breaking the consent redirect URL
// match for custom-domain MCP clients.
func TestHandleIDPCallback_UsesBaseURLFromCachedState(t *testing.T) {
	t.Parallel()

	gramUserID := "user-" + uuid.New().String()[:8]
	mock := &mockIdentityResolver{
		exchangeResult: &identity.IDPUserInfo{
			Sub:   "workos-user-baseurl",
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

	const customDomainBaseURL = "https://gram.custom.example.com"
	challengeID := uuid.NewString()
	err := ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: toolset.CustomDomainID,
			BaseURL:        customDomainBaseURL,
			RouteBase:      "mcp",
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
		"code":  {"idp-auth-code-baseurl"},
	}
	// Request arrives at the global /mcp/idp_callback URL — no
	// customdomains.Context, so without the snapshot the redirect
	// would fall back to s.serverURL.
	req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?"+q.Encode(), nil)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, chi.NewRouteContext()))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, customDomainBaseURL+"/"), "consent redirect must start with the cached BaseURL (custom domain origin); got %q", loc)
	require.NotContains(t, loc, "0.0.0.0", "consent redirect must not fall back to the server default origin when BaseURL is set")
	require.Contains(t, loc, "/connect", "should redirect to consent page")
	require.Contains(t, loc, "/"+mcpSlug+"/connect", "should include the slug under /mcp")
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
		Endpoint: mcp.EndpointRef{
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
		Endpoint: mcp.EndpointRef{
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

// HandleIDPCallback is mounted at the global server URL and never has a
// custom-domain context, so its forwarded-error response is the most exposed
// of the four.
func TestHandleIDPCallback_IDPErrorIssUsesMintOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	const mintOrigin = "https://idp-cb.customer.example"

	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        mcpSlug,
			CustomDomainID: toolset.CustomDomainID,
			BaseURL:        mintOrigin,
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-token",
		CreatedAt:           time.Now(),
	}))

	q := url.Values{
		"state":             {stateID},
		"error":             {"access_denied"},
		"error_description": {"user cancelled"},
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?"+q.Encode(), nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "access_denied", loc.Query().Get("error"))
	require.Equal(t, mintOrigin+"/mcp/"+mcpSlug, loc.Query().Get("iss"),
		"the IDP callback has no custom-domain context and must use the mint-time snapshot")
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
		Endpoint: mcp.EndpointRef{
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
		Endpoint: mcp.EndpointRef{
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
		Endpoint: mcp.EndpointRef{
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

// dcrPublicClientBody is a minimal RFC 7591 registration request for a
// public (PKCE-only) client — enough to pass RegistrationRequest.Validate
// so the test reaches the resolution path under test.
const dcrPublicClientBody = `{"client_name":"age-2640 test","redirect_uris":["http://localhost:3000/callback"],"token_endpoint_auth_method":"none"}`

// newRegisterRequest builds a POST /mcp/{slug}/register request carrying a
// JSON DCR body and the chi mcpSlug route param. The handler call is left to
// each test so resolution errors surface in the test function itself.
func newRegisterRequest(t *testing.T, slug, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	return req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
}

// TestHandleRegister_RemoteBackedIssuerGated_ResolvesViaEndpoints is the
// direct AGE-2640 repro: a remote-backed, issuer-gated mcp_server has no
// toolsets row, so the legacy toolset-only resolver 404'd at DCR. It must
// now resolve via mcp_endpoints → mcp_servers and complete registration.
//
// HandleRegister is the representative OAuth flow handler here: register,
// authorize, consent, token, and revoke all share the same
// LoadResolvedMcpEndpointBySlug resolution path.
func TestHandleRegister_RemoteBackedIssuerGated_ResolvesViaEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "remote-register-" + uuid.NewString()
	createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "public", issuerID)

	w := httptest.NewRecorder()
	err := ti.service.HandleRegister(w, newRegisterRequest(t, slug, dcrPublicClientBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["client_id"])
}

// TestHandleRegister_LegacyToolsetSlug_ResolvesViaFallback is the regression
// guard for issuer-gated toolset-backed servers that predate the toolsets →
// mcp_servers migration: they have no mcp_endpoint row, so the addressing
// lookup misses and resolution must fall back to toolsets.mcp_slug.
func TestHandleRegister_LegacyToolsetSlug_ResolvesViaFallback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	w := httptest.NewRecorder()
	err := ti.service.HandleRegister(w, newRegisterRequest(t, toolset.McpSlug.String, dcrPublicClientBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["client_id"])
}

// TestHandleRegister_UnknownSlug_ReturnsNotFound confirms a slug matching
// neither an mcp_endpoint nor a toolset still 404s after the addressing miss
// falls through the toolset fallback.
func TestHandleRegister_UnknownSlug_ReturnsNotFound(t *testing.T) {
	t.Parallel()

	_, ti := newTestMCPService(t)
	slug := "definitely-missing-" + uuid.NewString()[:8]

	w := httptest.NewRecorder()
	err := ti.service.HandleRegister(w, newRegisterRequest(t, slug, dcrPublicClientBody))
	require.Error(t, err)
	require.Empty(t, w.Body.String())

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// seedConsentChallenge stores a ready-to-consent AuthnChallengeState and
// returns its state id and CSRF token.
func seedConsentChallenge(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, client usersessions_repo.UserSessionClient) (string, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockidp.MockUserID, toolset.ID.String())
	subject := urn.NewUserSubject(mockidp.MockUserID)
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
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
	}))
	return stateID, csrfToken
}

func seedModernConsentChallenge(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	issuerID uuid.UUID,
	client usersessions_repo.UserSessionClient,
	mcpServerID uuid.UUID,
	endpointSlug string,
) string {
	t.Helper()

	subject := urn.NewUserSubject(mockidp.MockUserID)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: issuerID,
		Endpoint: mcp.EndpointRef{
			RouteBase:   "x/mcp",
			McpSlug:     endpointSlug,
			McpServerID: uuid.NullUUID{UUID: mcpServerID, Valid: true},
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           "csrf-" + uuid.NewString(),
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))
	return stateID
}

func consentGetPageWithContext(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, stateID string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/connect?state="+stateID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func consentGetPage(t *testing.T, ti *testInstance, mcpSlug, stateID string) string {
	t.Helper()
	return consentGetPageWithContext(t, t.Context(), ti, mcpSlug, stateID)
}

func modernConsentGetPage(t *testing.T, ctx context.Context, ti *testInstance, endpointSlug, stateID string) string {
	t.Helper()

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, endpointSlug, "x/mcp")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/x/mcp/"+endpointSlug+"/connect?state="+stateID, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(w, req, endpoint))
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func consentApproveButtonTag(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `value="approve"`)
	require.GreaterOrEqual(t, start, 0, "approve button must render")
	end := strings.Index(page[start:], ">")
	require.GreaterOrEqual(t, end, 0)
	return page[start : start+end]
}

func TestHandleConsentGet_LegacyToolsetUsesAllToolsConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID, _ := seedConsentChallenge(t, ctx, ti, toolset, client)

	page := consentGetPage(t, ti, toolset.McpSlug.String, stateID)
	require.NotContains(t, page, "consent-tools-root")
	require.NotContains(t, page, "Tool access")
	require.NotContains(t, consentApproveButtonTag(t, page), "disabled", "unrestricted consent remains available")
}

func TestHandleConsentGet_ModernToolsetWithoutProxyShowsToolPicker(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpointSlug := "modern-clean-" + uuid.NewString()
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuer.ID)
	stateID := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, mcpServer.ID, endpointSlug)

	page := modernConsentGetPage(t, ctx, ti, endpointSlug, stateID)
	require.Contains(t, page, "consent-tools-root")
	require.Contains(t, consentApproveButtonTag(t, page), "disabled", "island owns enabling the approve button")
}

func TestHandleConsentGet_ModernToolsetWithProxyUsesAllToolsConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	proxyToolset := setupToolsetWithExternalMCP(
		t,
		ctx,
		ti,
		"https://upstream.invalid/mcp",
		externalmcp_types.TransportTypeStreamableHTTP,
		"consent-proxy-"+uuid.NewString()[:8],
	).toolset
	endpointSlug := "modern-proxy-" + uuid.NewString()
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, proxyToolset.ProjectID, proxyToolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuer.ID)
	stateID := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, mcpServer.ID, endpointSlug)

	page := modernConsentGetPage(t, ctx, ti, endpointSlug, stateID)
	require.NotContains(t, page, "consent-tools-root")
	require.NotContains(t, page, "Tool access")
	require.NotContains(t, consentApproveButtonTag(t, page), "disabled", "unrestricted consent remains available")
}

// TestHandleConsentGet_CustomDomainLockdownBlocksLegacyInventory keeps the
// unrestricted legacy consent page usable while the direct inventory
// transport remains protected outside an organization's IP allowlist.
func TestHandleConsentGet_CustomDomainLockdownBlocksLegacyInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, domain := attachCustomDomainToToolset(t, ctx, ti, authCtx, toolset, "consent-lockdown-"+uuid.NewString()+".example.com")
	_, err := customdomains_repo.New(ti.conn).UpdateCustomDomainIPAllowlist(ctx, customdomains_repo.UpdateCustomDomainIPAllowlistParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		IpAllowlist:    []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)
	stateID, csrfToken := seedConsentChallenge(t, ctx, ti, toolset, client)

	page := consentGetPageWithContext(t, context.Background(), ti, toolset.McpSlug.String, stateID)
	require.NotContains(t, page, "consent-tools-root")
	require.NotContains(t, consentApproveButtonTag(t, page), "disabled")

	customCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	page = consentGetPageWithContext(t, customCtx, ti, toolset.McpSlug.String, stateID)
	require.NotContains(t, page, "consent-tools-root")
	require.NotContains(t, consentApproveButtonTag(t, page), "disabled")

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Consent-State", stateID)
	req.Header.Set("Gram-Consent-Csrf", csrfToken)
	req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	err = ti.service.HandleConsentMCP(httptest.NewRecorder(), req)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestHandleConsentMCP_PrivateToolsetRequiresConnect ensures consent-time
// enumeration applies the same server-level gate as runtime dispatch. Tool
// names must not leak through roleHidden metadata to a subject with no
// mcp:connect grant.
func TestHandleConsentMCP_PrivateToolsetRequiresConnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.addToolWithSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	subject := urn.NewUserSubject("ungranted-user-" + uuid.NewString())
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: mcp.EndpointRef{
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
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Consent-State", stateID)
	req.Header.Set("Gram-Consent-Csrf", csrfToken)
	req.Header.Set("Gram-Consent-Inventory-Attempt", uuid.NewString())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleConsentMCP(w, req)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.NotContains(t, w.Body.String(), `"tools"`)
	require.NotContains(t, w.Body.String(), "roleHiddenTools")
}

// TestHandleConsentPost_FilteringOnWithoutInventoryConflicts asserts a
// restrictive approve cannot skip the display-to-grant binding: filtering=on
// without a bound inventory attempt is a retryable conflict that leaves the
// challenge unconsumed.
func TestHandleConsentPost_FilteringOnWithoutInventoryConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID, csrfToken := seedConsentChallenge(t, ctx, ti, toolset, client)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	form.Set("tool_filtering", "on")
	form.Set("tool_selection_mode", "tools")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	err := ti.service.HandleConsent(httptest.NewRecorder(), req)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)

	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err, "conflict must not consume the challenge")
}

// TestHandleConsentPost_ApproveWithToolFilteringBindsSelection walks the
// restrictive approve end to end: hydrate the inventory over the consent
// transport, submit tools mode bound to that attempt, and assert the minted
// grant carries a resource-bound restrictive selection (submitted names
// outside the snapshot intersected away).
func TestHandleConsentPost_ApproveWithToolFilteringBindsSelection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	stateID, csrfToken := seedConsentChallenge(t, ctx, ti, toolset, client)

	attempt := hydrateConsentInventory(t, ctx, ti, toolset.McpSlug.String, stateID, csrfToken)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	form.Set("tool_inventory_id", attempt)
	form.Set("tool_filtering", "on")
	form.Add("tools", "not-in-inventory")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "userSessionGrant:"+toolset.UserSessionIssuerID.UUID.String()+":"+code)
	require.NoError(t, err)
	require.NotNil(t, grant.ToolSelection, "restrictive approve must persist a selection")
	require.Equal(t, "toolset:"+toolset.ID.String(), grant.ToolSelection.Resource)
	require.NotEqual(t, uuid.Nil, grant.ToolSelection.GrantID)
	require.Empty(t, grant.ToolSelection.Allow, "names outside the snapshot are intersected away")
	require.False(t, grant.ToolSelection.AllowsName("not-in-inventory"))
}

// seedUserSessionWithSelection inserts a live user_sessions row carrying the
// given tool_selection document and returns its raw refresh token.
func seedUserSessionWithSelection(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, clientRowID uuid.UUID, selection []byte) string {
	t.Helper()

	subject := urn.NewUserSubject("refresh-user-" + uuid.NewString())
	refreshToken := "refresh-" + uuid.NewString()
	sum := sha256.Sum256([]byte(refreshToken))
	_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID,
		UserSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
		SubjectUrn:          subject,
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    base64.RawURLEncoding.EncodeToString(sum[:]),
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
		ToolSelection:       selection,
	})
	require.NoError(t, err)
	return refreshToken
}

func postRefreshGrant(t *testing.T, ti *testInstance, mcpSlug, clientID, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, req))
	return w
}

// TestHandleTokenCode_ToolSelectionResourceBinding asserts an authorization
// code carrying a selection consented on a sibling endpoint (codes are
// cached issuer-wide) fails redemption with invalid_grant instead of
// minting a token that would immediately fail the serve path's resource
// check.
func TestHandleTokenCode_ToolSelectionResourceBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)

	redeem := func(resource string) *httptest.ResponseRecorder {
		selection, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
			{Type: toolfilter.AllowTypeTool, Name: "a", Mode: nil, Tools: nil},
		})
		require.NoError(t, err)
		code := "code-" + uuid.NewString()
		require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
			Code:                        code,
			FlowID:                      "",
			UserSessionIssuerID:         toolset.UserSessionIssuerID.UUID,
			UserSessionClientID:         client.ID,
			ClientID:                    client.ClientID,
			RedirectURI:                 "http://127.0.0.1:51423/callback",
			CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
			CodeChallengeMethod:         "S256",
			Subject:                     urn.NewUserSubject("code-user-" + uuid.NewString()),
			DesiredSessionDurationHours: 0,
			ToolSelection:               selection,
			CreatedAt:                   time.Now(),
		}))

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", code)
		form.Set("redirect_uri", "http://127.0.0.1:51423/callback")
		form.Set("client_id", client.ClientID)
		form.Set("code_verifier", verifier)
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleToken(w, req))
		return w
	}

	w := redeem("toolset:" + uuid.NewString())
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_grant")
	require.Contains(t, w.Body.String(), "different MCP endpoint")

	w = redeem("toolset:" + toolset.ID.String())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

// TestHandleTokenRefresh_ToolSelectionResourceBinding asserts a restrictive
// selection slides through refresh only on the endpoint it was consented on:
// a sibling endpoint sharing the issuer gets invalid_grant instead of a
// token that would immediately fail the serve path's resource check.
func TestHandleTokenRefresh_ToolSelectionResourceBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	mismatched := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"tool","name":"a"}]}`, uuid.NewString(), uuid.NewString())
	var err error
	_ = err
	w := postRefreshGrant(t, ti, toolset.McpSlug.String, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, mismatched))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_grant")
	require.Contains(t, w.Body.String(), "different MCP endpoint")

	matching := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"tool","name":"a"}]}`, toolset.ID.String(), uuid.NewString())
	w = postRefreshGrant(t, ti, toolset.McpSlug.String, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, matching))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}
