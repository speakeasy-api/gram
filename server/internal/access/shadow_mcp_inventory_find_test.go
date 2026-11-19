package access

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_FindShadowMCPInventoryServers_MatchesSameUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	seedShadowMCPInventoryServer(t, ctx, ti, projectID, "Tracker")
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  projectID,
		ServerURL:  "https://github.example.com/mcp",
		ServerName: "Tracker",
		UserEmail:  "reader@example.com",
		ObservedAt: time.Now().UTC(),
	})

	// The catalog's endpoint differs in case, port and trailing slash, yet
	// names the same upstream. An unrelated URL and a malformed one match
	// nothing.
	catalogURL := "HTTPS://GitHub.example.com:443/mcp/"
	result, err := ti.service.FindShadowMCPInventoryServers(ctx, &gen.FindShadowMCPInventoryServersPayload{
		SessionToken: nil,
		ProjectID:    projectID,
		ServerUrls:   []string{"https://other.example.com/mcp", catalogURL, "not a url"},
	})
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	match := result.Matches[0]
	require.Equal(t, catalogURL, match.ServerURL)
	require.Equal(t, "https://github.example.com/mcp", match.Server.CanonicalServerURL)
	require.Equal(t, "github-example-com-mcp-d8860eea", match.Server.ServerSlug)
	require.Equal(t, 1, match.Server.UserCount)
	require.Nil(t, match.Server.OfficialServer)

	// Once a server is linked, the match reports it, so the catalog can tell
	// the Shadow MCP server is already replaced.
	official := seedAccessOfficialServer(t, ctx, ti, *authCtx.ProjectID, "Tracker Official", "https://github.example.com/mcp", "private")
	require.NoError(t, ti.service.SetShadowMCPOfficialServer(ctx, &gen.SetShadowMCPOfficialServerPayload{
		SessionToken:      nil,
		ProjectID:         projectID,
		ServerURL:         catalogURL,
		McpServerID:       new(official.ID.String()),
		AllowDirectAccess: false,
	}))
	result, err = ti.service.FindShadowMCPInventoryServers(ctx, &gen.FindShadowMCPInventoryServersPayload{
		SessionToken: nil,
		ProjectID:    projectID,
		ServerUrls:   []string{catalogURL},
	})
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.NotNil(t, result.Matches[0].Server.OfficialServer)
	require.Equal(t, official.ID.String(), result.Matches[0].Server.OfficialServer.McpServerID)
	require.False(t, result.Matches[0].Server.OfficialServer.DirectAccessAllowed)
}

func TestService_FindShadowMCPInventoryServers_NoMatchWithoutUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)

	result, err := ti.service.FindShadowMCPInventoryServers(ctx, &gen.FindShadowMCPInventoryServersPayload{
		SessionToken: nil,
		ProjectID:    authCtx.ProjectID.String(),
		ServerUrls:   []string{"https://github.example.com/mcp"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Matches)
}

func TestService_FindShadowMCPInventoryServers_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	_, err := ti.service.FindShadowMCPInventoryServers(ctx, &gen.FindShadowMCPInventoryServersPayload{
		SessionToken: nil,
		ProjectID:    authCtx.ProjectID.String(),
		ServerUrls:   []string{"https://github.example.com/mcp"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
