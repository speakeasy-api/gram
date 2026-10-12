package plugins_test

import (
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// createCanonicalWrapper gives a toolset its canonical server (id = toolset id) with one endpoint per slug.
func createCanonicalWrapper(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsetsrepo.Toolset, visibility string, domainID uuid.NullUUID, slugs ...string) {
	t.Helper()
	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: visibility,
	})
	require.NoError(t, err)
	for _, slug := range slugs {
		_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
			ProjectID: toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, CustomDomainID: domainID, Slug: slug,
		})
		require.NoError(t, err)
	}
}

func addToolsetKeyedEntry(t *testing.T, ctx context.Context, ti *testInstance, pluginID string, toolsetID uuid.UUID, name string) pluginsrepo.PluginServer {
	t.Helper()
	row, err := pluginsrepo.New(ti.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: uuid.MustParse(pluginID), ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		McpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, DisplayName: name, Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
	return row
}

var manifestVersion = regexp.MustCompile(`\b1\d{12}\b`)

func normalizedFiles(files map[string][]byte) map[string]string {
	out := make(map[string]string, len(files))
	for path, content := range files {
		out[path] = manifestVersion.ReplaceAllString(string(content), "<version>")
	}
	return out
}

// The dependent move re-keys toolset-keyed rows onto the canonical wrapper; the
// published packages must not change when it does.
func TestPluginsService_PublishPlugins_HostedEntryIdenticalAfterRekey(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createTestToolset(t, ctx, ti.conn, "rekeyed")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{McpIsPublic: true, ID: toolset.ID, ProjectID: toolset.ProjectID}))
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: ac.ActiveOrganizationID, Domain: "hosted-" + uuid.NewString()[:8] + ".example.test", ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	//nolint:glint // notestingrawsql: an addressable domain without exercising domain provisioning.
	_, err = ti.conn.Exec(ctx, `UPDATE custom_domains SET verified = TRUE, activated = TRUE WHERE id = $1`, domain.ID)
	require.NoError(t, err)
	//nolint:glint // notestingrawsql: hosted toolsets take their domain through the toolset row.
	_, err = ti.conn.Exec(ctx, `UPDATE toolsets SET custom_domain_id = $1 WHERE id = $2`, domain.ID, toolset.ID)
	require.NoError(t, err)
	createCanonicalWrapper(t, ctx, ti, toolset, "public", uuid.NullUUID{UUID: domain.ID, Valid: true}, toolset.McpSlug.String)

	metadata, err := mcpmetarepo.New(ti.conn).UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: *ac.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{Valid: false}, ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID: uuid.NullUUID{Valid: false}, Instructions: pgtype.Text{Valid: false}, DefaultEnvironmentID: uuid.NullUUID{Valid: false},
		InstallationOverrideUrl: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	_, err = mcpmetarepo.New(ti.conn).UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID: *ac.ProjectID, McpMetadataID: metadata.ID, VariableName: "HOSTED_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "Authorization", Valid: true}, ProvidedBy: "user",
	})
	require.NoError(t, err)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Rekeyed"})
	require.NoError(t, err)
	entry := addToolsetKeyedEntry(t, ctx, ti, plugin.ID, toolset.ID, "Hosted")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	before := maps.Clone(mock.lastPushedFiles)
	var config struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(before[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://"+domain.Domain+"/mcp/"+toolset.McpSlug.String, config.MCPServers["Hosted"].URL)
	require.Contains(t, string(before[plugin.Slug+"/.mcp.json"]), "${user_config.HOSTED_API_KEY}")

	//nolint:glint // notestingrawsql: mirrors the dependent move, which re-keys rows in place.
	_, err = ti.conn.Exec(ctx, `UPDATE plugin_servers SET toolset_id = NULL, mcp_server_id = $1 WHERE id = $2`, toolset.ID, entry.ID)
	require.NoError(t, err)
	//nolint:glint // notestingrawsql: mirrors the dependent move, which re-keys rows in place.
	_, err = ti.conn.Exec(ctx, `UPDATE mcp_metadata SET toolset_id = NULL, mcp_server_id = $1 WHERE id = $2`, toolset.ID, metadata.ID)
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, normalizedFiles(before), normalizedFiles(mock.lastPushedFiles))
}

func TestPluginsService_PublishPlugins_HostedEntryPublishesPrimaryEndpoint(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	toolset := createTestToolset(t, ctx, ti.conn, "primary")
	createCanonicalWrapper(t, ctx, ti, toolset, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, toolset.McpSlug.String, toolset.McpSlug.String+"-second")
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Primary"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(toolset.ID.String()), DisplayName: conv.PtrEmpty("Hosted"), Policy: "required"})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	var config struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://app.getgram.ai/mcp/"+toolset.McpSlug.String, config.MCPServers["Hosted"].URL)
}

// A canonical wrapper that lost its endpoint keeps the toolset's address under either key.
func TestPluginsService_PublishPlugins_EndpointlessHostedEntryKeepsToolsetAddress(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	toolset := createTestToolset(t, ctx, ti.conn, "endpointless")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{McpIsPublic: true, ID: toolset.ID, ProjectID: toolset.ProjectID}))
	createCanonicalWrapper(t, ctx, ti, toolset, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Endpointless"})
	require.NoError(t, err)
	entry := addToolsetKeyedEntry(t, ctx, ti, plugin.ID, toolset.ID, "Hosted")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	before := maps.Clone(mock.lastPushedFiles)
	require.Contains(t, string(before[plugin.Slug+"/.mcp.json"]), "https://app.getgram.ai/mcp/"+toolset.McpSlug.String)

	//nolint:glint // notestingrawsql: mirrors the dependent move, which re-keys rows in place.
	_, err = ti.conn.Exec(ctx, `UPDATE plugin_servers SET toolset_id = NULL, mcp_server_id = $1 WHERE id = $2`, toolset.ID, entry.ID)
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, normalizedFiles(before), normalizedFiles(mock.lastPushedFiles))
}

func TestPluginsService_AddPluginServer_RejectsTwinsUnderEitherKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Keys"})
	require.NoError(t, err)
	requireConflict := func(err error) {
		t.Helper()
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeConflict, shareable.Code)
	}

	byToolset := createTestToolset(t, ctx, ti.conn, "by-toolset")
	createCanonicalWrapper(t, ctx, ti, byToolset, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, byToolset.McpSlug.String)
	added, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(byToolset.ID.String()), Policy: "required"})
	require.NoError(t, err)
	require.Equal(t, byToolset.ID.String(), *added.ToolsetID, "toolset attaches keep the key older servers read")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(byToolset.ID.String()), DisplayName: conv.PtrEmpty("Twin"), Policy: "required"})
	requireConflict(err)

	byServer := createTestToolset(t, ctx, ti.conn, "by-server")
	createCanonicalWrapper(t, ctx, ti, byServer, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, byServer.McpSlug.String)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(byServer.ID.String()), Policy: "required"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(byServer.ID.String()), DisplayName: conv.PtrEmpty("Twin 2"), Policy: "required"})
	requireConflict(err)
}
