package mcp_test

import (
	"context"
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

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestAuthorize_CustomDomainPrivateChallengeUsesGramIDPCallback(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "auth-callback-cd-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	toolset, domain := attachCustomDomainToToolset(t, ctx, ti, authCtx, toolset, "auth-callback.example.com")
	clientID := "custom-domain-client"
	clientRedirectURI := "http://example.com/cb"
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)

	customCtx := customdomains.WithContext(context.Background(), &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", clientRedirectURI)
	q.Set("state", "state-123")
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(customCtx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	redirectURI, err := url.Parse(loc.Query().Get("redirect_uri"))
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.Scheme, redirectURI.Scheme)
	require.Equal(t, ti.serverURL.Host, redirectURI.Host)
	require.Equal(t, "/mcp/idp_callback", redirectURI.Path)
	require.NotEqual(t, domain.Domain, redirectURI.Host)

	_, authnCache := buildChallengeManagerForTest(t, ti)
	stored, err := authnCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, toolset.McpSlug.String, stored.Endpoint.McpSlug)
	require.True(t, stored.Endpoint.CustomDomainID.Valid)
	require.Equal(t, domain.ID, stored.Endpoint.CustomDomainID.UUID)
	// The snapshot every later response in this flow derives its origin from.
	// Stamping the platform origin here would make each of them emit an `iss`
	// the client discards, with no error it is allowed to display.
	require.Equal(t, "https://"+domain.Domain, stored.Endpoint.BaseURL)
}

// The end-to-end shape the mint-time snapshot exists for: /authorize runs on a
// custom domain and the consent POST completing the flow arrives on the
// platform origin, which is what the remote-session return leg produces. The
// `iss` the client finally receives has to be the custom-domain issuer it
// recorded from the metadata document, not the origin of whichever request
// happened to finish the flow.
func TestAuthorize_CustomDomainIssSurvivesPlatformOriginConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "iss-cd-e2e-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	// Public so /authorize stamps an anonymous subject and goes straight to
	// consent, keeping this test on the origin question rather than the IDP
	// round-trip. The local copy is updated too because attachCustomDomainToToolset
	// writes the whole row back from the struct it is handed.
	require.NoError(t, toolsets_repo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))
	toolset.McpIsPublic = true
	toolset, domain := attachCustomDomainToToolset(t, ctx, ti, authCtx, toolset, "iss-cd-e2e.example.com")

	clientID := "iss-e2e-client"
	// The redirect_uri insertUserSessionClient registers for this client.
	clientRedirectURI := "http://example.com/cb"
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)

	domainCtx := customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})

	// What the client records before it ever calls /authorize.
	advertisedIssuer, supported := fetchAdvertisedIssuer(t, domainCtx, ti, slug)
	require.Equal(t, true, supported)
	require.Equal(t, "https://"+domain.Domain+"/mcp/"+slug, advertisedIssuer)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", clientRedirectURI)
	q.Set("state", "client-state")
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	authReq := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/authorize?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	authReq = authReq.WithContext(context.WithValue(domainCtx, chi.RouteCtxKey, rctx))

	authResp := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(authResp, authReq))
	require.Equal(t, http.StatusFound, authResp.Code)

	consentLoc, err := url.Parse(authResp.Header().Get("Location"))
	require.NoError(t, err)
	require.Contains(t, consentLoc.Path, "/connect")
	stateID := consentLoc.Query().Get("state")
	require.NotEmpty(t, stateID)

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)

	// The consent POST arrives with no custom-domain context at all.
	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", stored.CSRFToken)
	form.Set("action", "approve")
	consentReq := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
	consentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consentRctx := chi.NewRouteContext()
	consentRctx.URLParams.Add("mcpSlug", slug)
	consentReq = consentReq.WithContext(context.WithValue(ctx, chi.RouteCtxKey, consentRctx))

	consentResp := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(consentResp, consentReq))
	require.Equal(t, http.StatusSeeOther, consentResp.Code)

	clientLoc, err := url.Parse(consentResp.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, clientLoc.Query().Get("code"))
	require.Equal(t, advertisedIssuer, clientLoc.Query().Get("iss"))
}

func TestIDPCallback_StaticRouteResolvesToolsetFromChallengeState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "idp-static-callback-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	toolset, domain := attachCustomDomainToToolset(t, ctx, ti, authCtx, toolset, "idp-static-callback.example.com")

	_, authnCache := buildChallengeManagerForTest(t, ti)
	stateID := uuid.NewString()
	clientRedirectURI := "http://example.com/cb"
	require.NoError(t, authnCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: issuer.ID,
		Endpoint: mcp.EndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		},
		ClientID:            "test-mcp-client",
		RedirectURI:         clientRedirectURI,
		State:               "client-state",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             nil,
		CreatedAt:           time.Now(),
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?state="+stateID+"&error=access_denied", nil)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleIDPCallback(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, clientRedirectURI, loc.Scheme+"://"+loc.Host+loc.Path)
	require.Equal(t, "access_denied", loc.Query().Get("error"))
	require.Equal(t, "client-state", loc.Query().Get("state"))
}

// Public /authorize must not upgrade ambient credentials (cookie, header,
// Bearer) to a user subject. The opt-in path is `?requireUserIdentity=1`.
func TestAuthorize_PublicToolset_IgnoresAmbientSessionCredentials(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "auth-public-anon-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	require.NoError(t, toolsets_repo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))
	toolset.McpIsPublic = true

	clientID := "public-anon-client"
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)

	sessionToken := ti.getSessionToken(ctx, t)
	bearerUserID := uuid.NewString()
	bearerUserToken := mintUserSessionBearerForSubject(t, ti, toolset, urn.NewUserSubject(bearerUserID))

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "http://example.com/cb")
	q.Set("state", "state-"+uuid.NewString()[:8])
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil)

	req.Header.Set("Authorization", "Bearer "+bearerUserToken)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	requestCtx := contextvalues.SetSessionTokenInContext(context.Background(), sessionToken)
	req = req.WithContext(context.WithValue(requestCtx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Contains(t, loc.Path, "/connect", "public toolset without opt-in must redirect to consent")

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	require.NotNil(t, stored.Subject)
	require.Equal(t, urn.SessionSubjectKindAnonymous, stored.Subject.Kind,
		"public /authorize must not promote ambient credentials to a user subject")
	require.NotEqual(t, authCtx.UserID, stored.Subject.ID)
	require.NotEqual(t, bearerUserID, stored.Subject.ID)
}

// Authorize rejects in two shapes — inline JSON while the redirect_uri is
// still untrusted, and a redirect once it is. Only the redirect shape is an
// authorization response, and it is the one that carries iss.
func TestAuthorize_PostRedirectErrorEmitsIss(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	// redirect_uri is registered, so validation moves past the inline-error
	// phase; response_type=token then fails ValidatePostRedirect and must be
	// reported by redirect rather than inline.
	q := url.Values{}
	q.Set("response_type", "token")
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", client.RedirectUris[0])
	q.Set("state", "client-state")
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code, "a trusted redirect_uri means errors go back by redirect")

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, loc.Query().Get("error"))
	require.Equal(t, "client-state", loc.Query().Get("state"))
	require.Equal(t, advertisedIssuer, loc.Query().Get("iss"))
}

func createPrivateIssuerGatedToolset(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	slug string,
) (toolsets_repo.Toolset, usersessions_repo.UserSessionIssuer) {
	t.Helper()

	usersRepo := usersessions_repo.New(ti.conn)
	issuer, err := usersRepo.CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "usi-" + uuid.New().String()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: int64(time.Hour / time.Microsecond),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	require.NoError(t, err)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Private Issuer MCP " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("A private issuer-gated MCP for auth testing"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	toolset, err = toolsetsRepo.UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)

	return toolset, issuer
}

// RFC 8707 §2 rejection is a post-redirect outcome: the redirect_uri has
// already been matched against the registered set, so the client learns about
// it by 302 carrying invalid_target, never inline.
func TestAuthorize_MismatchedResourceRejectedByRedirect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", client.RedirectUris[0])
	q.Set("state", "client-state")
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	q.Set("resource", "https://someone-else.example.com/mcp/"+mcpSlug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code, "a trusted redirect_uri means errors go back by redirect")

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "invalid_target", loc.Query().Get("error"))
	require.Equal(t, "client-state", loc.Query().Get("state"))
	require.Equal(t, advertisedIssuer, loc.Query().Get("iss"))
}

// The value a conformant client sends is the one it read from the
// protected-resource metadata, so echoing that back must start the flow.
func TestAuthorize_MatchingResourceStartsFlow(t *testing.T) {
	t.Parallel()

	idpURL, err := url.Parse("https://idp.example.test/authorize")
	require.NoError(t, err)

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{buildAuthURLResult: idpURL})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", client.RedirectUris[0])
	q.Set("state", "client-state")
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	q.Set("resource", advertisedIssuer)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+mcpSlug+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Empty(t, loc.Query().Get("error"), "matching resource must not be rejected")
	require.Equal(t, idpURL.Host, loc.Host, "a matching resource must carry the flow on to the IDP")
}
