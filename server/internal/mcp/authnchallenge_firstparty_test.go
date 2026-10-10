package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// Dashboard-started connects for remote servers must stay on the /mcp surface.
func TestHandleFirstPartyConnect_RemoteMcpEndpointStaysOnMcpSurface(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "first-party-remote-" + uuid.NewString()[:8]
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.invalid/mcp", slug, "private", issuerID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/connect/first-party", nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleFirstPartyConnect(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	callback, err := url.Parse(loc.Query().Get("redirect_uri"))
	require.NoError(t, err)
	require.Equal(t, "/mcp/idp_callback", callback.Path)

	stored, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	require.True(t, stored.FirstParty)
	require.Equal(t, "mcp", stored.Endpoint.RouteBase)
	require.Equal(t, slug, stored.Endpoint.McpSlug)
	require.True(t, stored.Endpoint.McpServerID.Valid)
	require.Equal(t, mcpServer.ID, stored.Endpoint.McpServerID.UUID)
	require.Equal(t, issuerID, stored.UserSessionIssuerID)
}
