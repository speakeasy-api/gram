package plugins_test

import (
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
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
	ctx, ti := newServerKeyPluginsService(t)

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
	stored, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, uuid.MustParse(plugin.ID))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, uuid.NullUUID{UUID: byToolset.ID, Valid: true}, stored[0].McpServerID, "a wrapped toolset is stored under its canonical server")
	require.False(t, stored[0].ToolsetID.Valid)
	require.Equal(t, new(byToolset.ID.String()), added.ToolsetID)
	require.Equal(t, new(byToolset.ID.String()), added.McpServerID)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(byToolset.ID.String()), DisplayName: conv.PtrEmpty("Twin"), Policy: "required"})
	requireConflict(err)

	byServer := createTestToolset(t, ctx, ti.conn, "by-server")
	createCanonicalWrapper(t, ctx, ti, byServer, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, byServer.McpSlug.String)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(byServer.ID.String()), Policy: "required"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(byServer.ID.String()), DisplayName: conv.PtrEmpty("Twin 2"), Policy: "required"})
	requireConflict(err)

	legacy := createTestToolset(t, ctx, ti.conn, "by-legacy")
	addToolsetKeyedEntry(t, ctx, ti, plugin.ID, legacy.ID, "Legacy")
	createCanonicalWrapper(t, ctx, ti, legacy, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, legacy.McpSlug.String)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(legacy.ID.String()), DisplayName: conv.PtrEmpty("Twin 3"), Policy: "required"})
	requireConflict(err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(legacy.ID.String()), DisplayName: conv.PtrEmpty("Twin 4"), Policy: "required"})
	requireConflict(err)

	unwrapped := createTestToolset(t, ctx, ti.conn, "unwrapped")
	added, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(unwrapped.ID.String()), Policy: "required"})
	require.NoError(t, err)
	require.NotNil(t, added.ToolsetID, "a toolset without a canonical server keeps the toolset key")
	require.Equal(t, unwrapped.ID.String(), *added.ToolsetID)
	require.Nil(t, added.McpServerID)

	// A canonical server disabled out of sync with its toolset would drop the entry from packages; keep the toolset key.
	drifted := createTestToolset(t, ctx, ti.conn, "drifted")
	createCanonicalWrapper(t, ctx, ti, drifted, "disabled", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	added, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(drifted.ID.String()), Policy: "required"})
	require.NoError(t, err)
	stored, err = pluginsrepo.New(ti.conn).ListPluginServers(ctx, uuid.MustParse(plugin.ID))
	require.NoError(t, err)
	idx := slices.IndexFunc(stored, func(row pluginsrepo.PluginServer) bool { return row.ID.String() == added.ID })
	require.GreaterOrEqual(t, idx, 0)
	require.Equal(t, uuid.NullUUID{UUID: drifted.ID, Valid: true}, stored[idx].ToolsetID)
	require.Equal(t, new(drifted.ID.String()), added.ToolsetID)
	require.Equal(t, new(drifted.ID.String()), added.McpServerID)
}

// A server-keyed metadata twin must not add headers to a toolset whose own metadata still serves.
func TestPluginsService_PublishPlugins_HostedEntryIgnoresMetadataTwin(t *testing.T) {
	t.Parallel()
	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createTestToolset(t, ctx, ti.conn, "metadata-twin")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{McpIsPublic: true, ID: toolset.ID, ProjectID: toolset.ProjectID}))
	createCanonicalWrapper(t, ctx, ti, toolset, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, toolset.McpSlug.String)

	meta := mcpmetarepo.New(ti.conn)
	owned, err := meta.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: *ac.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{Valid: false}, ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID: uuid.NullUUID{Valid: false}, Instructions: pgtype.Text{Valid: false}, DefaultEnvironmentID: uuid.NullUUID{Valid: false},
		InstallationOverrideUrl: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	twin, err := meta.UpsertMetadataByMcpServerID(ctx, mcpmetarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: *ac.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{Valid: false}, ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID: uuid.NullUUID{Valid: false}, Instructions: pgtype.Text{Valid: false}, DefaultEnvironmentID: uuid.NullUUID{Valid: false},
		InstallationOverrideUrl: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	for _, cfg := range []struct {
		metadataID uuid.UUID
		variable   string
		header     string
	}{{owned.ID, "OWNED_KEY", "X-Owned"}, {twin.ID, "TWIN_KEY", "X-Twin"}} {
		_, err = meta.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
			ProjectID: *ac.ProjectID, McpMetadataID: cfg.metadataID, VariableName: cfg.variable,
			HeaderDisplayName: pgtype.Text{String: cfg.header, Valid: true}, ProvidedBy: "user",
		})
		require.NoError(t, err)
	}

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Metadata Twin"})
	require.NoError(t, err)
	addToolsetKeyedEntry(t, ctx, ti, plugin.ID, toolset.ID, "Hosted")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	config := string(mock.lastPushedFiles[plugin.Slug+"/.mcp.json"])
	require.Contains(t, config, "${user_config.OWNED_KEY}")
	require.NotContains(t, config, "TWIN_KEY")
}

// User configs on only the server-keyed twin must not block an entry whose published output reads the toolset-keyed row.
func TestPluginsService_CompatibilityIgnoresMetadataTwinHeaders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createTestToolset(t, ctx, ti.conn, "twin-headers")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{McpIsPublic: true, ID: toolset.ID, ProjectID: toolset.ProjectID}))
	createCanonicalWrapper(t, ctx, ti, toolset, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, toolset.McpSlug.String)

	meta := mcpmetarepo.New(ti.conn)
	_, err := meta.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: *ac.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{Valid: false}, ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID: uuid.NullUUID{Valid: false}, Instructions: pgtype.Text{Valid: false}, DefaultEnvironmentID: uuid.NullUUID{Valid: false},
		InstallationOverrideUrl: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	twin, err := meta.UpsertMetadataByMcpServerID(ctx, mcpmetarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: *ac.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{Valid: false}, ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID: uuid.NullUUID{Valid: false}, Instructions: pgtype.Text{Valid: false}, DefaultEnvironmentID: uuid.NullUUID{Valid: false},
		InstallationOverrideUrl: pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	_, err = meta.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID: *ac.ProjectID, McpMetadataID: twin.ID, VariableName: "TWIN_KEY",
		HeaderDisplayName: pgtype.Text{String: "X-Twin", Valid: true}, ProvidedBy: "user",
	})
	require.NoError(t, err)

	byToolset, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Twin By Toolset"})
	require.NoError(t, err)
	addToolsetKeyedEntry(t, ctx, ti, byToolset.ID, toolset.ID, "Hosted")
	byServer, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Twin By Server"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: byServer.ID, McpServerID: conv.PtrEmpty(toolset.ID.String()), DisplayName: conv.PtrEmpty("Hosted"), Policy: "required"})
	require.NoError(t, err)

	issues, err := pluginsrepo.New(ti.conn).ListAgentPluginCompatibilityIssuesForProject(ctx, pluginsrepo.ListAgentPluginCompatibilityIssuesForProjectParams{
		ProjectID: *ac.ProjectID, PluginIds: []uuid.UUID{uuid.MustParse(byToolset.ID), uuid.MustParse(byServer.ID)},
	})
	require.NoError(t, err)
	require.Empty(t, issues)
}
