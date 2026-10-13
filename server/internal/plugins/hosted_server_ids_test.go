package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type backendIDs struct {
	toolsetID   *string
	mcpServerID *string
}

func entryIDs(servers []*gen.PluginServer) map[string]backendIDs {
	out := make(map[string]backendIDs, len(servers))
	for _, s := range servers {
		out[s.DisplayName] = backendIDs{toolsetID: s.ToolsetID, mcpServerID: s.McpServerID}
	}
	return out
}

// Callers that read toolset_id keep getting it for a hosted entry stored under its canonical server.
func TestPluginsService_HostedEntriesReportBothIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newServerKeyPluginsService(t)

	serverKeyed := createTestToolset(t, ctx, ti.conn, "server-keyed")
	createCanonicalWrapper(t, ctx, ti, serverKeyed, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, serverKeyed.McpSlug.String)
	toolsetKeyed := createTestToolset(t, ctx, ti.conn, "toolset-keyed")
	createCanonicalWrapper(t, ctx, ti, toolsetKeyed, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, toolsetKeyed.McpSlug.String)
	unwrapped := createTestToolset(t, ctx, ti.conn, "unwrapped")
	deletedWrapper := createTestToolset(t, ctx, ti.conn, "deleted-wrapper")
	createCanonicalWrapper(t, ctx, ti, deletedWrapper, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deletedWrapper.ID, ProjectID: deletedWrapper.ProjectID})
	require.NoError(t, err)
	fresh := createTestMcpServer(t, ctx, ti.conn, "fresh", "private")

	orphaned := createTestToolset(t, ctx, ti.conn, "orphaned")
	createCanonicalWrapper(t, ctx, ti, orphaned, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Both IDs"})
	require.NoError(t, err)

	added, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(serverKeyed.ID.String()), DisplayName: conv.PtrEmpty("ServerKeyed"), Policy: "required"})
	require.NoError(t, err)
	stored, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, uuid.MustParse(plugin.ID))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.False(t, stored[0].ToolsetID.Valid, "the phase B writer stores the server key")
	require.Equal(t, serverKeyed.ID.String(), conv.PtrValOr(added.ToolsetID, ""))
	require.Equal(t, serverKeyed.ID.String(), conv.PtrValOr(added.McpServerID, ""))

	toolsetKeyedEntry := addToolsetKeyedEntry(t, ctx, ti, plugin.ID, toolsetKeyed.ID, "ToolsetKeyed")
	unwrappedEntry := addToolsetKeyedEntry(t, ctx, ti, plugin.ID, unwrapped.ID, "Unwrapped")
	addToolsetKeyedEntry(t, ctx, ti, plugin.ID, deletedWrapper.ID, "DeletedWrapper")
	addToolsetKeyedEntry(t, ctx, ti, plugin.ID, orphaned.ID, "Orphaned")
	// A legacy entry can outlive its toolset; its wrapper does not make it an MCP server entry.
	_, err = toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: orphaned.Slug, ProjectID: orphaned.ProjectID})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(fresh.idStr), DisplayName: conv.PtrEmpty("Fresh"), Policy: "required"})
	require.NoError(t, err)

	want := map[string]backendIDs{
		"ServerKeyed":    {toolsetID: new(serverKeyed.ID.String()), mcpServerID: new(serverKeyed.ID.String())},
		"ToolsetKeyed":   {toolsetID: new(toolsetKeyed.ID.String()), mcpServerID: new(toolsetKeyed.ID.String())},
		"Unwrapped":      {toolsetID: new(unwrapped.ID.String()), mcpServerID: nil},
		"DeletedWrapper": {toolsetID: new(deletedWrapper.ID.String()), mcpServerID: nil},
		"Orphaned":       {toolsetID: new(orphaned.ID.String()), mcpServerID: nil},
		"Fresh":          {toolsetID: nil, mcpServerID: new(fresh.idStr)},
	}

	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Equal(t, want, entryIDs(got.Servers))

	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var listed *gen.Plugin
	for _, p := range list.Plugins {
		if p.ID == plugin.ID {
			listed = p
		}
	}
	require.NotNil(t, listed)
	require.Equal(t, want, entryIDs(listed.Servers))

	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Both IDs", Slug: got.Slug})
	require.NoError(t, err)
	require.Equal(t, want, entryIDs(updated.Servers))

	for name, id := range map[string]string{"ServerKeyed": added.ID, "ToolsetKeyed": toolsetKeyedEntry.ID.String(), "Unwrapped": unwrappedEntry.ID.String()} {
		updatedEntry, err := ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{ID: id, PluginID: plugin.ID, DisplayName: name, Policy: "optional", SortOrder: 0})
		require.NoError(t, err)
		wantEntry := want[name]
		require.Equal(t, wantEntry, backendIDs{toolsetID: updatedEntry.ToolsetID, mcpServerID: updatedEntry.McpServerID}, name)
	}
}
