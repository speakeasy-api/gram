package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type backendIDs struct {
	toolsetID   *string
	mcpServerID *string
}

func entryIDs(t *testing.T, servers []*gen.PluginServer) map[string]backendIDs {
	t.Helper()
	out := make(map[string]backendIDs, len(servers))
	for _, s := range servers {
		require.NotContains(t, out, s.DisplayName, "display names are unique per entry")
		out[s.DisplayName] = backendIDs{toolsetID: s.ToolsetID, mcpServerID: s.McpServerID}
	}
	return out
}

// Callers that read toolset_id keep getting it for a hosted entry stored under its canonical server.
func TestPluginsService_HostedEntriesReportBothIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	serverKeyed := createUnwrappedTestToolset(t, ctx, ti.conn, "server-keyed")
	createCanonicalWrapper(t, ctx, ti, serverKeyed, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, serverKeyed.Slug)
	fresh := createTestMcpServer(t, ctx, ti.conn, "fresh", "private")

	orphaned := createUnwrappedTestToolset(t, ctx, ti.conn, "orphaned")
	createCanonicalWrapper(t, ctx, ti, orphaned, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Both IDs"})
	require.NoError(t, err)

	added, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(serverKeyed.ID.String()), DisplayName: conv.PtrEmpty("ServerKeyed"), Policy: "required"})
	require.NoError(t, err)
	stored, err := pluginsrepo.New(ti.conn).ListPluginServers(ctx, uuid.MustParse(plugin.ID))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, serverKeyed.ID.String(), conv.PtrValOr(added.ToolsetID, ""))
	require.Equal(t, serverKeyed.ID.String(), conv.PtrValOr(added.McpServerID, ""))

	byToolset := createUnwrappedTestToolset(t, ctx, ti.conn, "by-toolset")
	createCanonicalWrapper(t, ctx, ti, byToolset, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, byToolset.Slug)
	addedByToolset, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(byToolset.ID.String()), DisplayName: conv.PtrEmpty("ByToolset"), Policy: "required"})
	require.NoError(t, err)
	require.Equal(t, new(byToolset.ID.String()), addedByToolset.ToolsetID)
	require.Equal(t, new(byToolset.ID.String()), addedByToolset.McpServerID)

	// An entry echoed from a read carries both ids; equal ids name the toolset, unequal ones are rejected.
	echoed := createUnwrappedTestToolset(t, ctx, ti.conn, "echoed")
	createCanonicalWrapper(t, ctx, ti, echoed, "private", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, echoed.Slug)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(echoed.ID.String()), McpServerID: conv.PtrEmpty(fresh.idStr), DisplayName: conv.PtrEmpty("Mismatched"), Policy: "required"})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	addedEchoed, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(echoed.ID.String()), McpServerID: conv.PtrEmpty(echoed.ID.String()), DisplayName: conv.PtrEmpty("Echoed"), Policy: "required"})
	require.NoError(t, err)
	require.Equal(t, new(echoed.ID.String()), addedEchoed.ToolsetID)
	require.Equal(t, new(echoed.ID.String()), addedEchoed.McpServerID)

	_, err = pluginsrepo.New(ti.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: uuid.MustParse(plugin.ID), McpServerID: uuid.NullUUID{UUID: orphaned.ID, Valid: true}, DisplayName: "Orphaned", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
	// A wrapper whose toolset is gone no longer reports a toolset id.
	_, err = toolsetsrepo.New(ti.conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: orphaned.Slug, ProjectID: orphaned.ProjectID})
	require.NoError(t, err)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{PluginID: plugin.ID, McpServerID: conv.PtrEmpty(fresh.idStr), DisplayName: conv.PtrEmpty("Fresh"), Policy: "required"})
	require.NoError(t, err)

	want := map[string]backendIDs{
		"ServerKeyed": {toolsetID: new(serverKeyed.ID.String()), mcpServerID: new(serverKeyed.ID.String())},
		"ByToolset":   {toolsetID: new(byToolset.ID.String()), mcpServerID: new(byToolset.ID.String())},
		"Echoed":      {toolsetID: new(echoed.ID.String()), mcpServerID: new(echoed.ID.String())},
		"Orphaned":    {toolsetID: nil, mcpServerID: new(orphaned.ID.String())},
		"Fresh":       {toolsetID: nil, mcpServerID: new(fresh.idStr)},
	}

	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Equal(t, want, entryIDs(t, got.Servers))

	list, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var listed *gen.Plugin
	for _, p := range list.Plugins {
		if p.ID == plugin.ID {
			listed = p
		}
	}
	require.NotNil(t, listed)
	require.Equal(t, want, entryIDs(t, listed.Servers))

	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Both IDs", Slug: got.Slug})
	require.NoError(t, err)
	require.Equal(t, want, entryIDs(t, updated.Servers))

	for name, id := range map[string]string{"ServerKeyed": added.ID, "ByToolset": addedByToolset.ID} {
		updatedEntry, err := ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{ID: id, PluginID: plugin.ID, DisplayName: name, Policy: "optional", SortOrder: 0})
		require.NoError(t, err)
		wantEntry := want[name]
		require.Equal(t, wantEntry, backendIDs{toolsetID: updatedEntry.ToolsetID, mcpServerID: updatedEntry.McpServerID}, name)
	}
}
