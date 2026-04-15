package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestPluginsService_CreatePlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	result, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{
		Name: "Engineering Tools",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Engineering Tools", result.Name)
	require.Equal(t, "engineering-tools", result.Slug)
}

func TestPluginsService_CreatePlugin_DuplicateSlugReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{
		Name: "Duplicate Test",
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{
		Name: "Duplicate Test",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestPluginsService_CreatePlugin_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	ctx = access.GrantsToContext(ctx, &access.Grants{})

	_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{
		Name: "Forbidden Plugin",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestPluginsService_GetPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{
		Name: "Get Test",
	})
	require.NoError(t, err)

	result, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, result.ID)
	require.Equal(t, "Get Test", result.Name)
}

func TestPluginsService_GetPlugin_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: uuid.New().String()})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestPluginsService_ListPlugins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "List A"})
	require.NoError(t, err)
	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "List B"})
	require.NoError(t, err)

	result, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Plugins), 2)
}

func TestPluginsService_UpdatePlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Before Update"})
	require.NoError(t, err)

	desc := "updated description"
	updated, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID:          created.ID,
		Name:        "After Update",
		Slug:        created.Slug,
		Description: &desc,
	})
	require.NoError(t, err)
	require.Equal(t, "After Update", updated.Name)
	require.NotNil(t, updated.Description)
	require.Equal(t, "updated description", *updated.Description)
}

func TestPluginsService_UpdatePlugin_DuplicateSlugReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "First Plugin"})
	require.NoError(t, err)

	second, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Second Plugin"})
	require.NoError(t, err)

	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID:   second.ID,
		Name: second.Name,
		Slug: "first-plugin", // collides with first
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestPluginsService_DeletePlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "To Delete"})
	require.NoError(t, err)

	err = ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: created.ID})
	require.NoError(t, err)

	// Should be gone now.
	_, err = ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: created.ID})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestPluginsService_AddPluginServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Server Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "test-toolset")
	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "My Toolset Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	require.Equal(t, "My Toolset Server", server.DisplayName)
	require.Equal(t, "required", server.Policy)
	require.Equal(t, toolset.ID.String(), server.ToolsetID)
}

func TestPluginsService_UpdatePluginServer_VerifiesOwnership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Ownership Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "ownership-toolset")
	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "My Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	// Try to update with a non-existent plugin ID — should fail.
	_, err = ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{
		ID:          server.ID,
		PluginID:    uuid.New().String(),
		DisplayName: "Hacked",
		Policy:      "required",
		SortOrder:   0,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestPluginsService_RemovePluginServer_VerifiesOwnership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Remove Ownership"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "remove-toolset")
	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "My Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	// Try to remove with a non-existent plugin ID — should fail.
	err = ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{
		ID:       server.ID,
		PluginID: uuid.New().String(),
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestPluginsService_SetPluginAssignments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Assignment Test"})
	require.NoError(t, err)

	// Set initial assignments.
	result, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"role:engineering", "role:gtm"},
	})
	require.NoError(t, err)
	require.Len(t, result.Assignments, 2)

	// Replace with different assignments.
	result, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"*"},
	})
	require.NoError(t, err)
	require.Len(t, result.Assignments, 1)
	require.Equal(t, "*", result.Assignments[0].PrincipalUrn)

	// Verify old assignments are gone by fetching the plugin.
	fetched, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, fetched.Assignments, 1)
}

func TestPluginsService_SetPluginAssignments_NonExistentPluginReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      uuid.New().String(),
		PrincipalUrns: []string{"*"},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestPluginsService_SetPluginAssignments_InvalidURNReturnsBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "URN Validation"})
	require.NoError(t, err)

	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"role:engineering", "not a valid urn"},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestPluginsService_DownloadPluginPackage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_ = authCtx

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Download Test"})
	require.NoError(t, err)

	result, body, err := ti.service.DownloadPluginPackage(ctx, &gen.DownloadPluginPackagePayload{
		PluginID: plugin.ID,
		Platform: "claude",
	})
	require.NoError(t, err)
	require.Equal(t, "application/zip", result.ContentType)
	require.Contains(t, result.ContentDisposition, "download-test.zip")
	require.NotNil(t, body)
	require.NoError(t, body.Close())
}
