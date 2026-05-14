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

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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
		Endpoint: mcp.LegacyMcpEndpointRef{
			McpSlug:        toolset.McpSlug.String,
			CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		},
		ClientID:            "test-mcp-client",
		RedirectURI:         clientRedirectURI,
		State:               "client-state",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
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
