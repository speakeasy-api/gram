package plugins_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// serverEntry extracts one server's raw JSON entry from a pushed MCP config so
// parity can be asserted byte-for-byte per server, independent of other keys
// (API keys differ between publishes for private servers, so whole-file
// comparison would be too strict in general).
func serverEntry(t *testing.T, files map[string][]byte, path, displayName string) json.RawMessage {
	t.Helper()

	raw := files[path]
	require.NotNil(t, raw, "missing pushed file %s", path)

	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &config))
	entry, ok := config.MCPServers[displayName]
	require.True(t, ok, "server %q missing from %s", displayName, path)
	return entry
}

// A wrapped toolset must publish identically whether its plugin_servers row is
// keyed by toolset_id (direct mcp_slug publishing) or by the wrapper
// mcp_server_id, and whether its mcp_metadata is toolset- or server-keyed —
// the states before, during, and after the wraptoolsets backfill and its
// -move-plugins mode.
func TestPluginsService_PublishPlugins_ToolsetBackedWrapperParity(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Parity Test"})
	require.NoError(t, err)

	// A public toolset publishing through its mcp_slug with one user env config.
	toolset := createTestToolset(t, ctx, ti.conn, "parity-toolset")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))

	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{String: "", Valid: false},
		ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
		LogoID:                    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Instructions:              pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		InstallationOverrideUrl:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "PARITY_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "Authorization", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)

	toolsetKeyed, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Parity Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	claudePath := "parity-test/.mcp.json"
	cursorPath := "cursor-plugins/parity-test-cursor/mcp.json"

	// Phase A: toolset-keyed plugin server, toolset-keyed metadata.
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	claudeBaseline := serverEntry(t, mock.lastPushedFiles, claudePath, "Parity Server")
	cursorBaseline := serverEntry(t, mock.lastPushedFiles, cursorPath, "Parity Server")
	require.Contains(t, string(claudeBaseline), "${user_config.PARITY_API_KEY}")

	// Phase B: the backfill has created the wrapper (public, endpoint at the
	// toolset's published slug) and moved metadata server-side, but the
	// plugin_servers row is still toolset-keyed.
	wrapperID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    wrapperID,
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: toolset.Name, Valid: true},
		Slug:                  pgtype.Text{String: toolset.Slug + "-wrap", Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityPublic,
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    wrapperID,
		Slug:           toolset.McpSlug.String,
	})
	require.NoError(t, err)
	moved, err := mcpRepo.MoveMetadataToMcpServer(ctx, mcpmetarepo.MoveMetadataToMcpServerParams{
		McpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true},
		ToolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:   *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, moved)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.JSONEq(t, string(claudeBaseline), string(serverEntry(t, mock.lastPushedFiles, claudePath, "Parity Server")))
	require.JSONEq(t, string(cursorBaseline), string(serverEntry(t, mock.lastPushedFiles, cursorPath, "Parity Server")))

	// Phase C: the plugin_servers row has moved onto the wrapper
	// (-move-plugins), so generation takes the mcp_server-keyed branch.
	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{
		ID:       toolsetKeyed.ID,
		PluginID: plugin.ID,
	}))
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(wrapperID.String()),
		DisplayName: conv.PtrEmpty("Parity Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.JSONEq(t, string(claudeBaseline), string(serverEntry(t, mock.lastPushedFiles, claudePath, "Parity Server")))
	require.JSONEq(t, string(cursorBaseline), string(serverEntry(t, mock.lastPushedFiles, cursorPath, "Parity Server")))
}

// A private wrapped toolset must keep private semantics through the plugin
// wrapper branch: a baked Gram API key Authorization header, not the remote
// OAuth shape.
func TestPluginsService_PublishPlugins_ToolsetBackedWrapperPrivateParity(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Private Parity"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "private-parity")

	toolsetKeyed, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Private Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	claudePath := "private-parity/.mcp.json"

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	baseline := serverEntry(t, mock.lastPushedFiles, claudePath, "Private Server")
	require.Contains(t, string(baseline), "Authorization", "private toolset servers bake a Gram API key header")

	wrapperID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    wrapperID,
		ProjectID:             *authCtx.ProjectID,
		Name:                  pgtype.Text{String: toolset.Name, Valid: true},
		Slug:                  pgtype.Text{String: toolset.Slug + "-wrap", Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    wrapperID,
		Slug:           toolset.McpSlug.String,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{
		ID:       toolsetKeyed.ID,
		PluginID: plugin.ID,
	}))
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(wrapperID.String()),
		DisplayName: conv.PtrEmpty("Private Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	entry := serverEntry(t, mock.lastPushedFiles, claudePath, "Private Server")

	// Each publish mints a fresh baked key, so compare shape rather than
	// bytes: same URL, an Authorization header present, and no OAuth stdio
	// (command) form.
	var baselineEntry, wrapperEntry struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Command string            `json:"command"`
		Headers map[string]string `json:"headers"`
	}
	require.NoError(t, json.Unmarshal(baseline, &baselineEntry))
	require.NoError(t, json.Unmarshal(entry, &wrapperEntry))
	require.Equal(t, baselineEntry.Type, wrapperEntry.Type)
	require.Equal(t, baselineEntry.URL, wrapperEntry.URL)
	require.Empty(t, wrapperEntry.Command)
	require.NotEmpty(t, wrapperEntry.Headers["Authorization"])
	require.NotEmpty(t, baselineEntry.Headers["Authorization"])
}
