package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// A toolset in a deleted project answers every legacy slug entry point exactly
// as an unknown slug does.
func TestLegacyFallback_DeletedProjectAnswersAsUnknownSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	public := createPublicMCPToolset(t, ctx, ti, authCtx, "deleted-proj-public-"+uuid.NewString()[:8])
	gated, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	unknown := "deleted-proj-unknown-" + uuid.NewString()[:8]

	w, err := servePublicHTTP(t, context.Background(), ti, public.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w, err = runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, gated.McpSlug.String)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	gatedRef := mcp.EndpointRef{McpSlug: gated.McpSlug.String, RouteBase: mcproute.MCP, ToolsetID: conv.ToNullUUID(gated.ID)}
	_, err = ti.service.LoadResolvedMcpEndpointByRef(ctx, gatedRef)
	require.NoError(t, err)

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)

	for _, slug := range []string{unknown, public.McpSlug.String, gated.McpSlug.String} {
		_, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
		requireOopsCode(t, err, oops.CodeNotFound)

		for _, handler := range []func(http.ResponseWriter, *http.Request) error{
			ti.service.HandleGetProtectedResource,
			ti.service.HandleGetAuthorizationServer,
		} {
			_, err := runMCPWellKnown(t, ctx, handler, slug)
			requireOopsCode(t, err, oops.CodeNotFound)
		}

		_, err = ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, mcproute.MCP)
		requireOopsCode(t, err, oops.CodeNotFound)

		w := httptest.NewRecorder()
		err = ti.service.HandleRegister(w, newRegisterRequest(t, slug, dcrPublicClientBody))
		requireOopsCode(t, err, oops.CodeNotFound)
		require.Empty(t, w.Body.String())

		err = ti.service.HandleAuthorize(httptest.NewRecorder(), authorizeRequest(t, slug, client.ClientID, client.RedirectUris[0]))
		requireOopsCode(t, err, oops.CodeNotFound)

		err = ti.service.HandleToken(httptest.NewRecorder(), deletedProjectTokenRequest(t, slug, client.ClientID))
		requireOopsCode(t, err, oops.CodeNotFound)
	}

	clients, err := usersessions_repo.New(ti.conn).ListUserSessionClientsByProjectID(ctx, usersessions_repo.ListUserSessionClientsByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: conv.ToNullUUID(issuer.ID),
		Cursor:              uuid.NullUUID{},
		LimitValue:          10,
	})
	require.NoError(t, err)
	require.Len(t, clients, 1, "registration must not add a client for a deleted project")

	// A challenge minted before the delete must not resume after it.
	_, err = ti.service.LoadResolvedMcpEndpointByRef(ctx, gatedRef)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A deleted project's toolset keeps owning its slug on the platform host instead of
// yielding to a live custom-domain toolset that shares it.
func TestLegacyFallback_DeletedProjectSlugNotTakenOverByCustomDomainToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Live project", Slug: "live-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	otherAuth := *authCtx
	otherAuth.ProjectID = &other.ID
	slug := "deleted-proj-shared-" + uuid.NewString()[:8]
	live, domain := createPublicMCPToolsetWithCustomDomain(t, ctx, ti, &otherAuth, slug, slug+".example.com")
	createPublicMCPToolset(t, ctx, ti, authCtx, slug)

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	requireOopsCode(t, err, oops.CodeNotFound)

	customCtx := customdomains.WithContext(context.Background(), &customdomains.Context{OrganizationID: authCtx.ActiveOrganizationID, Domain: domain.Domain, DomainID: domain.ID})
	w, err := servePublicHTTP(t, customCtx, ti, live.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A custom-domain toolset in a deleted project is unknown on its own domain too.
func TestLegacyFallback_DeletedProjectCustomDomainToolset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "deleted-proj-cd-" + uuid.NewString()[:8]
	toolset, domain := createPublicMCPToolsetWithCustomDomain(t, ctx, ti, authCtx, slug, slug+".example.com")
	customCtx := customdomains.WithContext(context.Background(), &customdomains.Context{OrganizationID: authCtx.ActiveOrganizationID, Domain: domain.Domain, DomainID: domain.ID})
	w, err := servePublicHTTP(t, customCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)

	_, err = servePublicHTTP(t, customCtx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A meta MCP challenge minted before its project was deleted must not resume.
func TestLoadResolvedMcpEndpointByRef_MetaServerInDeletedProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "deleted-proj-meta-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	ref := mcp.EndpointRef{McpSlug: slug, RouteBase: mcproute.MCP, MetaMcpServerID: conv.ToNullUUID(meta.ID)}

	_, err := ti.service.LoadResolvedMcpEndpointByRef(ctx, ref)
	require.NoError(t, err)

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)

	_, err = ti.service.LoadResolvedMcpEndpointByRef(ctx, ref)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func deletedProjectTokenRequest(t *testing.T, slug, clientID string) *http.Request {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "not-a-real-code")
	form.Set("redirect_uri", "http://localhost:3000/callback")
	form.Set("client_id", clientID)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	return req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
}
