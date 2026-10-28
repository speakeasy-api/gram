package plugins_test

import (
	"context"
	"encoding/json"
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
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

func TestPluginsService_PublishPlugins_HostedEntryPublishesDomainAndUserHeaders(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createUnwrappedTestToolset(t, ctx, ti.conn, "hosted-domain")
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: ac.ActiveOrganizationID, Domain: "hosted-" + uuid.NewString()[:8] + ".example.test", ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	//nolint:glint // notestingrawsql: an addressable domain without exercising domain provisioning.
	_, err = ti.conn.Exec(ctx, `UPDATE custom_domains SET verified = TRUE, activated = TRUE WHERE id = $1`, domain.ID)
	require.NoError(t, err)
	createCanonicalWrapper(t, ctx, ti, toolset, "public", uuid.NullUUID{UUID: domain.ID, Valid: true}, toolset.McpSlug.String)

	metadata, err := mcpmetarepo.New(ti.conn).UpsertMetadataByMcpServerID(ctx, mcpmetarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID: toolset.ID, ProjectID: *ac.ProjectID,
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

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Hosted Domain"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), DisplayName: conv.PtrEmpty("Hosted"), Policy: "required"})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	var config struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"], &config))
	require.Equal(t, "https://"+domain.Domain+"/mcp/"+toolset.McpSlug.String, config.MCPServers["Hosted"].URL)
	require.Contains(t, string(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"]), "${user_config.HOSTED_API_KEY}")
}

func TestPluginsService_PublishPlugins_HostedEntryPublishesPrimaryEndpoint(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	toolset := createUnwrappedTestToolset(t, ctx, ti.conn, "primary")
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

// A canonical wrapper that lost its endpoint keeps the toolset's address.
func TestPluginsService_PublishPlugins_EndpointlessHostedEntryKeepsToolsetAddress(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	toolset := createUnwrappedTestToolset(t, ctx, ti.conn, "endpointless")
	createCanonicalWrapper(t, ctx, ti, toolset, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Endpointless"})
	require.NoError(t, err)
	_, err = pluginsrepo.New(ti.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: uuid.MustParse(plugin.ID), McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, DisplayName: "Hosted", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Contains(t, string(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"]), "https://app.getgram.ai/mcp/"+toolset.McpSlug.String)
}

func TestPluginsService_AddPluginServer_StoresToolsetsUnderTheirServer(t *testing.T) {
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

	byToolset := createUnwrappedTestToolset(t, ctx, ti.conn, "by-toolset")
	createCanonicalWrapper(t, ctx, ti, byToolset, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, byToolset.McpSlug.String)
	added, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(byToolset.ID.String()), Policy: "required"})
	require.NoError(t, err)
	stored, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, uuid.MustParse(plugin.ID))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, uuid.NullUUID{UUID: byToolset.ID, Valid: true}, stored[0].McpServerID, "a wrapped toolset is stored under its canonical server")
	require.Equal(t, new(byToolset.ID.String()), added.ToolsetID)
	require.Equal(t, new(byToolset.ID.String()), added.McpServerID)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(byToolset.ID.String()), DisplayName: conv.PtrEmpty("Twin"), Policy: "required"})
	requireConflict(err)

	byServer := createUnwrappedTestToolset(t, ctx, ti.conn, "by-server")
	createCanonicalWrapper(t, ctx, ti, byServer, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, byServer.McpSlug.String)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(byServer.ID.String()), Policy: "required"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(byServer.ID.String()), DisplayName: conv.PtrEmpty("Twin 2"), Policy: "required"})
	requireConflict(err)

	unwrapped := createUnwrappedTestToolset(t, ctx, ti.conn, "unwrapped")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(unwrapped.ID.String()), Policy: "required"})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code, "a toolset without an MCP server cannot be added")

	disabled := createUnwrappedTestToolset(t, ctx, ti.conn, "drifted")
	createCanonicalWrapper(t, ctx, ti, disabled, "disabled", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(disabled.ID.String()), Policy: "required"})
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code, "the canonical wrapper owns hosting state, so a disabled server cannot be added through its toolset")

	aliasOnly := createUnwrappedTestToolset(t, ctx, ti.conn, "alias-only")
	createCanonicalWrapper(t, ctx, ti, aliasOnly, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(aliasOnly.ID.String()), Policy: "required"})
	require.NoError(t, err, "an enabled server without an endpoint still serves on its legacy slug")
}

// A server that holds a toolset's id without being its canonical wrapper does not lend its metadata to the toolset environment.
func TestPluginsService_ToolsetEntryIgnoresMetadataOfNonCanonicalServerWithItsID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createUnwrappedTestToolset(t, ctx, ti.conn, "id-held")
	other := createUnwrappedTestToolset(t, ctx, ti.conn, "id-holder-backing")
	_, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, ToolsetID: uuid.NullUUID{UUID: other.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	meta := mcpmetarepo.New(ti.conn)
	held, err := meta.UpsertMetadataByMcpServerID(ctx, mcpmetarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID: toolset.ID, ProjectID: *ac.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{Valid: false}, ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID: uuid.NullUUID{Valid: false}, Instructions: pgtype.Text{Valid: false}, DefaultEnvironmentID: uuid.NullUUID{Valid: false},
		InstallationOverrideUrl: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	_, err = meta.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID: *ac.ProjectID, McpMetadataID: held.ID, VariableName: "HELD_KEY",
		HeaderDisplayName: pgtype.Text{String: "X-Held", Valid: true}, ProvidedBy: "user",
	})
	require.NoError(t, err)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Id Held"})
	require.NoError(t, err)
	_, err = pluginsrepo.New(ti.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: uuid.MustParse(plugin.ID), McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, DisplayName: "Id holder entry", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	pluginIDs := []uuid.UUID{uuid.MustParse(plugin.ID)}
	envs, err := pluginsrepo.New(ti.conn).ListPluginEnvironmentConfigsForProject(ctx, pluginsrepo.ListPluginEnvironmentConfigsForProjectParams{ProjectID: *ac.ProjectID, PluginIds: pluginIDs})
	require.NoError(t, err)
	require.Empty(t, envs, "the id holder's metadata is not its toolset's environment")
}
