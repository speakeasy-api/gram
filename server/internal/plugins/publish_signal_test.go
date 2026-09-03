package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// Every plugin mutation must enqueue a republish for its own project. Before
// this, propagation waited on a rollout sweep that had to run every few
// seconds to feel immediate; the sweep is now an hourly safety net, so a
// missing signal here means a change silently sits unpublished for an hour.
func TestPluginsService_MutationsSignalRepublish(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	assertSignalled := func(t *testing.T, count int, what string) {
		t.Helper()
		captured := ti.publisher.captured()
		require.Len(t, captured, count, "%s should enqueue a republish", what)
		last := captured[len(captured)-1]
		require.Equal(t, *authCtx.ProjectID, last.projectID)
		require.Equal(t, authCtx.UserID, last.createdByUserID)
	}

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Signalled"})
	require.NoError(t, err)
	assertSignalled(t, 1, "creating a plugin")

	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Signalled Renamed", Slug: "signalled-renamed"})
	require.NoError(t, err)
	assertSignalled(t, 2, "renaming a plugin")

	toolset := createTestToolset(t, ctx, ti.conn, "signalled-server")
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true, ID: toolset.ID, ProjectID: toolset.ProjectID,
	}))

	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID: plugin.ID, ToolsetID: conv.PtrEmpty(toolset.ID.String()), Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
	assertSignalled(t, 3, "adding a server to a plugin")

	_, err = ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{
		ID: server.ID, PluginID: plugin.ID, DisplayName: "Renamed Server", Policy: "optional", SortOrder: 1,
	})
	require.NoError(t, err)
	assertSignalled(t, 4, "updating a plugin server")

	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{ID: server.ID, PluginID: plugin.ID}))
	assertSignalled(t, 5, "removing a plugin server")

	require.NoError(t, ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: plugin.ID}))
	assertSignalled(t, 6, "deleting a plugin")
}

// ListPlugins lazily provisions the Default plugin for a project that predates
// it. That changes what a publish would generate, so it must signal like any
// explicit mutation — and must not keep signalling once the plugin exists.
func TestPluginsService_LazyDefaultPluginSignalsRepublish(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	_, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, ti.publisher.captured(), 1, "lazily creating the Default plugin should enqueue a republish")

	_, err = ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, ti.publisher.captured(), 1, "a read that creates nothing must not enqueue")
}
