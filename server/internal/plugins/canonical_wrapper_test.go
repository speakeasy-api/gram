package plugins_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestPluginsService_PublishPlugins_CanonicalWrapperWinsOverOtherToolsetServers(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createTestToolset(t, ctx, ti.conn, "canonical")
	servers := mcpserversrepo.New(ti.conn)
	// A gateway member over the same toolset, created before the canonical wrapper.
	_, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: *ac.ProjectID, Name: conv.ToPGText("gateway member"),
		Slug: conv.ToPGText("member-" + uuid.NewString()[:8]), ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility: "private",
	})
	require.NoError(t, err)
	_, err = servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: *ac.ProjectID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
		NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true},
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Slug: toolset.McpSlug.String,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: ac.ActiveOrganizationID, DnsName: pgtype.Text{String: "tail.example", Valid: true},
	}))

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Canonical"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), DisplayName: conv.PtrEmpty("Hosted"), Policy: "required",
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err, "a gateway member must not make the hosted toolset ambiguous")
	var config struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://tail.example/mcp/"+url.PathEscape(toolset.McpSlug.String), config.MCPServers["Hosted"].URL)
}
