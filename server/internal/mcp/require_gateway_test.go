package mcp_test

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestServePublic_RequireGatewayLiveSetting(t *testing.T) {
	t.Parallel()
	for _, wrapped := range []bool{false, true} {
		name := "toolset-slug"
		if wrapped {
			name = "server-endpoint"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "gateway-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), auth, slug)
			if wrapped {
				createToolsetMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, toolset.ID, slug, mcpservers.VisibilityPublic, uuid.NullUUID{}, uuid.Nil)
			}
			for _, enabled := range []bool{false, true, false} {
				_, err := projectsrepo.New(ti.conn).UpdateProject(ctx, projectsrepo.UpdateProjectParams{ProjectID: *auth.ProjectID, Name: "Test project", RequireMcpGateway: pgtype.Bool{Bool: enabled, Valid: true}})
				require.NoError(t, err)
				w, err := servePublicHTTP(t, ctx, ti, slug, makeInitializeBody(), "", map[string]string{"X-Speakeasy-Gateway": "true"})
				if enabled {
					var refusal *oops.ShareableError
					require.ErrorAs(t, err, &refusal)
					require.Equal(t, oops.CodeForbidden, refusal.Code)
					require.Contains(t, err.Error(), "gateway")
				} else {
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, w.Code)
				}
			}
		})
	}
}

func TestServePublic_RequireGatewayAllowsGatewayMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "gateway-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "Hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")
	_, err := projectsrepo.New(ti.conn).UpdateProject(ctx, projectsrepo.UpdateProjectParams{ProjectID: *auth.ProjectID, Name: "Test project", RequireMcpGateway: pgtype.Bool{Bool: true, Valid: true}})
	require.NoError(t, err)
	result := decodeMetaToolResult(t, callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": member.slug}))
	require.False(t, result.IsError)
	require.Contains(t, string(result.StructuredContent), "alpha_tool")
}

func TestServePublic_RequireGatewayBlocksRemoteMethods(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "remote-" + uuid.NewString()[:8]
			issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
			createRemoteMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, "https://example.com/mcp", slug, mcpservers.VisibilityPrivate, issuer)
			_, err := projectsrepo.New(ti.conn).UpdateProject(ctx, projectsrepo.UpdateProjectParams{ProjectID: *auth.ProjectID, Name: "Test project", RequireMcpGateway: pgtype.Bool{Bool: true, Valid: true}})
			require.NoError(t, err)
			if method == http.MethodPost {
				_, err = servePublicHTTP(t, ctx, ti, slug, makeInitializeBody(), "", nil)
			} else {
				_, err = serveRuntimeMethod(t, ctx, ti, method, slug, "", map[string]string{"Accept": "text/event-stream"})
			}
			var refusal *oops.ShareableError
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, oops.CodeForbidden, refusal.Code)
			require.Contains(t, err.Error(), "gateway")
		})
	}
}
