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
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// A toolset in a deleted project answers every legacy slug entry point exactly
// as an unknown slug does.
func TestLegacyFallback_DeletedProjectAnswersAsUnknownSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	public := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "deleted-proj-public-"+uuid.NewString()[:8])
	gated, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	unknown := "deleted-proj-unknown-" + uuid.NewString()[:8]

	w, err := servePublicHTTP(t, context.Background(), ti, public.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w, err = runMCPWellKnown(t, ctx, ti.service.HandleGetAuthorizationServer, gated.McpSlug.String)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

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

	var clients int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM user_session_clients WHERE user_session_issuer_id = $1`, issuer.ID).Scan(&clients))
	require.Equal(t, 1, clients, "registration must not add a client for a deleted project")

	// A challenge minted before the delete must not resume after it.
	_, err = ti.service.LoadResolvedMcpEndpointByRef(ctx, mcp.EndpointRef{McpSlug: gated.McpSlug.String, RouteBase: mcproute.MCP, ToolsetID: conv.ToNullUUID(gated.ID)})
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
