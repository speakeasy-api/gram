package plugins_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestPluginsService_PublishPlugins_PrivateOnlyUsesIngressEndpoint(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Private MCP"})
	require.NoError(t, err)
	server := createTestMcpServer(t, ctx, ti.conn, "Private API", mcpservers.VisibilityPrivate)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, McpServerID: conv.PtrEmpty(server.idStr), Policy: "required",
	})
	require.NoError(t, err)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: ac.ActiveOrganizationID, DnsName: pgtype.Text{String: "tail.example", Valid: true},
	}))
	rows, err := fixtures.SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
		NetworkAccessMode: pgtype.Text{String: "private_only", Valid: true}, ID: server.id, ProjectID: *ac.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	var config struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://tail.example/mcp/"+url.PathEscape(server.endpointSlug), config.MCPServers["Private API"].URL)

	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID: *ac.ProjectID, McpServerID: server.id,
	})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	_, err = mcpendpointsrepo.New(ti.conn).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{
		ID: endpoints[0].ID, ProjectID: *ac.ProjectID,
	})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.ErrorContains(t, errors.Unwrap(err), "no endpoint in the private ingress namespace")
}
