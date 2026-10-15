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
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestPluginsService_PublishPlugins_ReadsUserHeadersFromRekeyedMetadata(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createTestToolset(t, ctx, ti.conn, "rekeyed-toolset")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))
	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: *authCtx.ProjectID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)

	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "REKEYED_KEY",
		HeaderDisplayName: pgtype.Text{String: "X-Rekeyed", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)
	rows, err := testrepo.New(ti.conn).RekeyMcpMetadataToMcpServerFixture(ctx, testrepo.RekeyMcpMetadataToMcpServerFixtureParams{
		ProjectID: *authCtx.ProjectID,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Rekeyed"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Rekeyed Server"),
		Policy:      "required",
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	cursorMCP := mock.lastPushedFiles["cursor-plugins/rekeyed-cursor/mcp.json"]
	require.NotNil(t, cursorMCP)
	var cursorConfig struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(cursorMCP, &cursorConfig))
	require.Equal(t, "${env:REKEYED_KEY}", cursorConfig.MCPServers["Rekeyed Server"].Headers["X-Rekeyed"])
}

func TestPluginsService_PublishPlugins_UsesToolsetKeyedMetadataTwin(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createTestToolset(t, ctx, ti.conn, "twin-toolset")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))
	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: *authCtx.ProjectID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)

	mcpRepo := mcpmetarepo.New(ti.conn)
	toolsetRow, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	serverRow, err := mcpRepo.UpsertMetadataByMcpServerID(ctx, mcpmetarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:   *authCtx.ProjectID,
	})
	require.NoError(t, err)
	for metadataID, header := range map[uuid.UUID]string{toolsetRow.ID: "X-Toolset", serverRow.ID: "X-Server"} {
		_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
			ProjectID:         *authCtx.ProjectID,
			McpMetadataID:     metadataID,
			VariableName:      "TWIN_KEY",
			HeaderDisplayName: pgtype.Text{String: header, Valid: true},
			ProvidedBy:        "user",
		})
		require.NoError(t, err)
	}

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Twin"})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Twin Server"),
		Policy:      "required",
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	cursorMCP := mock.lastPushedFiles["cursor-plugins/twin-cursor/mcp.json"]
	require.NotNil(t, cursorMCP)
	var cursorConfig struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(cursorMCP, &cursorConfig))
	headers := cursorConfig.MCPServers["Twin Server"].Headers
	require.Equal(t, "${env:TWIN_KEY}", headers["X-Toolset"])
	require.NotContains(t, headers, "X-Server")
}
