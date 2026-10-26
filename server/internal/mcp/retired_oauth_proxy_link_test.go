package mcp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServePublic_IssuerGatedEndpointWithRetiredOAuthProxyLink(t *testing.T) {
	t.Parallel()

	for _, visibility := range []string{"public", "private"} {
		t.Run(visibility, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "proxy-link-"+uuid.NewString()[:8])
			testenv.LinkRetiredOAuthProxy(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)
			endpointSlug := "proxy-link-" + uuid.NewString()
			issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
			mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, visibility, uuid.NullUUID{}, issuerID)

			anonymous, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeToolsListBody(), "", nil)
			requireOopsCode(t, err, oops.CodeUnauthorized)
			require.NotEmpty(t, anonymous.Header().Get("WWW-Authenticate"))

			token := mintIssuerBearerForEndpointSubject(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID, urn.NewUserSubject(authCtx.UserID))
			signedIn, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeToolsListBody(), token, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, signedIn.Code)
			require.Empty(t, signedIn.Header().Get("WWW-Authenticate"))
		})
	}
}

// The legacy toolset-slug route runs the security check that used to read the link.
func TestServePublic_LegacyIssuerGatedToolsetWithRetiredOAuthProxyLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, token := createIssuerGatedLegacyToolset(t, ctx, ti, authCtx, "proxy-legacy-")
	testenv.LinkRetiredOAuthProxy(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID)

	anonymous, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeToolsListBody(), "", nil)
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.NotEmpty(t, anonymous.Header().Get("WWW-Authenticate"))

	signedIn, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, signedIn.Code)
}
