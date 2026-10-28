package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// The canonical server owns whether and how its endpoint serves.
func TestServePublic_EndpointFollowsCanonicalServerState(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		visibility string
		wantStatus int
	}{
		{name: "disabled server hides the endpoint", visibility: "disabled", wantStatus: 0},
		{name: "private server gates the endpoint", visibility: "private", wantStatus: http.StatusUnauthorized},
		{name: "public server serves anonymously", visibility: "public", wantStatus: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			slug := "server-state-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, ti, authCtx, slug)
			testenv.SetHostedMCP(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Visibility: tt.visibility})

			w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
			switch tt.wantStatus {
			case 0:
				require.Error(t, err)
				require.Contains(t, err.Error(), "not found")
			case http.StatusUnauthorized:
				require.Error(t, err, "anonymous request to a private server")
				require.Contains(t, err.Error(), "access token")
			default:
				require.NoError(t, err)
				require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
			}
		})
	}
}

// A toolset slug no endpoint holds is not served: hosted servers are reached only through their endpoints.
func TestRetiredToolsetSlug_NotFoundOnEverySurface(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "retired-toolset-slug-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, slug)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	setToolsetIssuer(t, ctx, ti.conn, toolset.ProjectID, toolset, issuerID)

	w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	require.Error(t, err, "issuer-gated endpoint asks for a token while it holds the slug")
	require.Contains(t, err.Error(), "access token")
	require.NotNil(t, w)

	require.NoError(t, testrepo.New(ti.conn).RetireHostedToolsetEndpointsFixture(ctx, testrepo.RetireHostedToolsetEndpointsFixtureParams{
		ID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: toolset.ProjectID,
	}))

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	requireOopsCode(t, err, oops.CodeNotFound)

	for _, handler := range []func(http.ResponseWriter, *http.Request) error{
		ti.service.HandleGetProtectedResource,
		ti.service.HandleGetAuthorizationServer,
	} {
		w, err := runMCPWellKnown(t, ctx, handler, slug)
		requireOopsCode(t, err, oops.CodeNotFound)
		require.Empty(t, w.Body.String())
	}

	_, err = ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, mcproute.MCP)
	requireOopsCode(t, err, oops.CodeNotFound)

	err = ti.service.HandleRegister(httptest.NewRecorder(), newRegisterRequest(t, slug, dcrPublicClientBody))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A challenge cached against a toolset with no MCP server id cannot resume.
func TestRetiredToolsetSlug_ToolsetChallengeDoesNotResume(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "retired-toolset-ref-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, slug)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	setToolsetIssuer(t, ctx, ti.conn, toolset.ProjectID, toolset, issuerID)

	_, err := ti.service.LoadResolvedMcpEndpointByRef(ctx, mcp.EndpointRef{McpSlug: slug, RouteBase: mcproute.MCP, McpServerID: conv.ToNullUUID(toolset.ID)})
	require.NoError(t, err)

	_, err = ti.service.LoadResolvedMcpEndpointByRef(ctx, mcp.EndpointRef{McpSlug: slug, RouteBase: mcproute.MCP, ToolsetID: conv.ToNullUUID(toolset.ID)})
	requireOopsCode(t, err, oops.CodeNotFound)
}
