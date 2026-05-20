package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
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

func TestAuthorize_PublicToolsetStampsSubject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "auth-public-" + uuid.New().String()[:8]
	toolset, issuer := createPublicIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	clientID := "public-client"
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)

	sessionToken := ti.getSessionToken(ctx, t)
	bearerUserID := uuid.NewString()
	bearerUserToken := mintUserSessionBearerForSubject(t, ti, toolset, urn.NewUserSubject(bearerUserID))
	anonBearerSubject := urn.NewAnonymousSubject(uuid.NewString())
	anonBearerToken := mintUserSessionBearerForSubject(t, ti, toolset, anonBearerSubject)

	cases := []struct {
		name      string
		setupCtx  func() context.Context
		mutate    func(*http.Request)
		wantKind  urn.SessionSubjectKind
		wantID    string
		notWantID string
	}{
		{
			name: "gram_session_header",
			mutate: func(r *http.Request) {
				r.Header.Set(constants.SessionHeader, sessionToken)
			},
			wantKind: urn.SessionSubjectKindUser,
			wantID:   authCtx.UserID,
		},
		{
			name: "session_token_in_context",
			setupCtx: func() context.Context {
				return contextvalues.SetSessionTokenInContext(context.Background(), sessionToken)
			},
			wantKind: urn.SessionSubjectKindUser,
			wantID:   authCtx.UserID,
		},
		{
			name:     "no_auth",
			wantKind: urn.SessionSubjectKindAnonymous,
		},
		{
			name: "invalid_session_header",
			mutate: func(r *http.Request) {
				r.Header.Set(constants.SessionHeader, "not-a-real-session-token")
			},
			wantKind: urn.SessionSubjectKindAnonymous,
		},
		{
			// Stale header shouldn't shadow a valid cookie on the same request.
			name: "stale_header_with_valid_cookie",
			setupCtx: func() context.Context {
				return contextvalues.SetSessionTokenInContext(context.Background(), sessionToken)
			},
			mutate: func(r *http.Request) {
				r.Header.Set(constants.SessionHeader, "not-a-real-session-token")
			},
			wantKind: urn.SessionSubjectKindUser,
			wantID:   authCtx.UserID,
		},
		{
			name: "user_bearer_jwt",
			mutate: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+bearerUserToken)
			},
			wantKind: urn.SessionSubjectKindUser,
			wantID:   bearerUserID,
		},
		{
			// A JWT whose subject is already anonymous shouldn't be honoured —
			// we'd just convert one anonymous URN into another. Re-mints fresh.
			name: "anonymous_bearer_jwt",
			mutate: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+anonBearerToken)
			},
			wantKind:  urn.SessionSubjectKindAnonymous,
			notWantID: anonBearerSubject.ID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var requestCtx context.Context
			if tc.setupCtx != nil {
				requestCtx = tc.setupCtx()
			}
			stored := drivePublicAuthorize(t, ctx, ti, toolset, clientID, requestCtx, tc.mutate)
			require.NotNil(t, stored.Subject)
			require.Equal(t, tc.wantKind, stored.Subject.Kind)
			if tc.wantID != "" {
				require.Equal(t, tc.wantID, stored.Subject.ID)
			}
			if tc.notWantID != "" {
				require.NotEqual(t, tc.notWantID, stored.Subject.ID)
			}
		})
	}
}

func drivePublicAuthorize(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	toolset toolsets_repo.Toolset,
	clientID string,
	requestCtx context.Context,
	mutate func(*http.Request),
) mcp.AuthnChallengeState {
	t.Helper()

	if requestCtx == nil {
		requestCtx = context.Background()
	}

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "http://example.com/cb")
	q.Set("state", "state-"+uuid.NewString()[:8])
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+q.Encode(), nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(requestCtx, chi.RouteCtxKey, rctx))

	if mutate != nil {
		mutate(req)
	}

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Contains(t, loc.Path, "/connect", "public toolset must redirect directly to consent, not the IDP")

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	return stored
}

func createPublicIssuerGatedToolset(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	slug string,
) (toolsets_repo.Toolset, usersessions_repo.UserSessionIssuer) {
	t.Helper()

	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	require.NoError(t, toolsets_repo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))
	toolset.McpIsPublic = true
	return toolset, issuer
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
