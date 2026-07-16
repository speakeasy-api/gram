package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// mockGitHubPublisher records calls for testing. Set the *Err fields to
// simulate GitHub-side failures.
type mockGitHubPublisher struct {
	createRepoCalled      bool
	pushFilesCalled       bool
	addCollaboratorCalled bool
	getRepoFilesCalled    bool
	collaborators         []string
	lastPushedFiles       map[string][]byte
	// repoFiles, when set, is returned by GetRepoFiles; otherwise it falls back
	// to lastPushedFiles so a second publish carries the first publish's files.
	repoFiles       map[string][]byte
	createRepoErr   error
	pushFilesErr    error
	getRepoFilesErr error
}

func (m *mockGitHubPublisher) CreateRepo(_ context.Context, _ int64, _, _ string, _ bool) error {
	m.createRepoCalled = true
	return m.createRepoErr
}

func (m *mockGitHubPublisher) PushFiles(_ context.Context, _ int64, _, _, _, _ string, files map[string][]byte) (string, error) {
	m.pushFilesCalled = true
	m.lastPushedFiles = files
	if m.pushFilesErr != nil {
		return "", m.pushFilesErr
	}
	return "abc123", nil
}

func (m *mockGitHubPublisher) AddCollaborator(_ context.Context, _ int64, _, _, username, _ string) error {
	m.addCollaboratorCalled = true
	m.collaborators = append(m.collaborators, username)
	return nil
}

func (m *mockGitHubPublisher) HasDirectCollaborator(_ context.Context, _ int64, _, _ string) (bool, error) {
	return len(m.collaborators) > 0, nil
}

func (m *mockGitHubPublisher) GetRepoFiles(_ context.Context, _ int64, _, _, _ string) (map[string][]byte, error) {
	m.getRepoFilesCalled = true
	if m.getRepoFilesErr != nil {
		return nil, m.getRepoFilesErr
	}
	if m.repoFiles != nil {
		return m.repoFiles, nil
	}
	if m.lastPushedFiles != nil {
		return m.lastPushedFiles, nil
	}
	return nil, ghclient.ErrRepoNotFound
}

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
	ctx = authz.GrantsToContext(ctx, nil)

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

// API key auth populates ProjectID but leaves ProjectSlug nil
// (server/internal/auth/key.go:168). Non-publish endpoints must still work
// in that mode — only PublishPlugins genuinely needs the slug.
func TestPluginsService_ReadEndpoints_WorkWithoutProjectSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "API Key Test"})
	require.NoError(t, err)

	// Simulate API key auth by clearing ProjectSlug on the existing context.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectSlug = nil
	apiKeyCtx := contextvalues.SetAuthContext(ctx, authCtx)

	_, err = ti.service.GetPlugin(apiKeyCtx, &gen.GetPluginPayload{ID: created.ID})
	require.NoError(t, err, "GetPlugin must work with API key auth (nil ProjectSlug)")

	_, err = ti.service.ListPlugins(apiKeyCtx, &gen.ListPluginsPayload{})
	require.NoError(t, err, "ListPlugins must work with API key auth (nil ProjectSlug)")
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
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("My Toolset Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	require.Equal(t, "My Toolset Server", server.DisplayName)
	require.Equal(t, "required", server.Policy)
	require.NotNil(t, server.ToolsetID)
	require.Equal(t, toolset.ID.String(), *server.ToolsetID)
	require.Nil(t, server.McpServerID)
}

func TestPluginsService_AddPluginServer_McpServerBacked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Remote Server Test"})
	require.NoError(t, err)

	mcpServer := createTestMcpServer(t, ctx, ti.conn, "Remote Widget", mcpservers.VisibilityPublic)

	// Omit display_name: it should default to the mcp_server's name.
	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	require.Equal(t, "Remote Widget", server.DisplayName)
	require.NotNil(t, server.McpServerID)
	require.Equal(t, mcpServer.idStr, *server.McpServerID)
	require.Nil(t, server.ToolsetID)
}

func TestPluginsService_AddPluginServer_DuplicateToolsetReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Dup Toolset"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "dup-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("First"),
		Policy:      "required",
	})
	require.NoError(t, err)

	// Adding the same toolset again (different display name) hits the
	// plugin_id+toolset_id unique index, not the display-name one.
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Second"),
		Policy:      "required",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "already been added")
}

func TestPluginsService_AddPluginServer_DuplicateMcpServerReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Dup Remote"})
	require.NoError(t, err)

	mcpServer := createTestMcpServer(t, ctx, ti.conn, "Dup Remote Server", mcpservers.VisibilityPublic)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("First"),
		Policy:      "required",
	})
	require.NoError(t, err)

	// Adding the same mcp_server again (different display name) hits the
	// plugin_id+mcp_server_id unique index, not the display-name one.
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("Second"),
		Policy:      "required",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "already been added")
}

func TestPluginsService_AddPluginServer_DuplicateDisplayNameReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Dup Name"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "dup-name-toolset")
	mcpServer := createTestMcpServer(t, ctx, ti.conn, "Dup Name Remote", mcpservers.VisibilityPublic)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Shared Name"),
		Policy:      "required",
	})
	require.NoError(t, err)

	// A different backend reusing the same display name hits the
	// plugin_id+display_name unique index (the default conflict branch).
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("Shared Name"),
		Policy:      "required",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "display name already exists")
}

func TestPluginsService_AddPluginServer_RequiresExactlyOneBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "XOR Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "xor-toolset")
	mcpServer := createTestMcpServer(t, ctx, ti.conn, "XOR Remote", mcpservers.VisibilityPublic)

	// Neither backend provided.
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		DisplayName: conv.PtrEmpty("No Backend"),
		Policy:      "required",
	})
	require.Error(t, err)
	var noBackendErr *oops.ShareableError
	require.ErrorAs(t, err, &noBackendErr)
	require.Equal(t, oops.CodeBadRequest, noBackendErr.Code)

	// Both backends provided.
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("Both Backends"),
		Policy:      "required",
	})
	require.Error(t, err)
	var bothErr *oops.ShareableError
	require.ErrorAs(t, err, &bothErr)
	require.Equal(t, oops.CodeBadRequest, bothErr.Code)
}

func TestPluginsService_AddPluginServer_RejectsDisabledMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Disabled Remote Test"})
	require.NoError(t, err)

	mcpServer := createTestMcpServer(t, ctx, ti.conn, "Disabled Remote", mcpservers.VisibilityDisabled)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("Disabled Server"),
		Policy:      "required",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestPluginsService_AddPluginServer_RejectsMcpServerWithoutEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "No Endpoint Test"})
	require.NoError(t, err)

	mcpServer := createTestMcpServerWithEndpoint(t, ctx, ti.conn, "No Endpoint Remote", mcpservers.VisibilityPublic, false)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("No Endpoint Server"),
		Policy:      "required",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestPluginsService_RemovePluginServer_McpServerBacked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Remove Remote Test"})
	require.NoError(t, err)

	mcpServer := createTestMcpServer(t, ctx, ti.conn, "Removable Remote", mcpservers.VisibilityPublic)
	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("Removable Server"),
		Policy:      "required",
	})
	require.NoError(t, err)

	err = ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{
		ID:       server.ID,
		PluginID: plugin.ID,
	})
	require.NoError(t, err)

	// The server is gone from the plugin.
	fetched, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Empty(t, fetched.Servers)
}

func TestPluginsService_UpdatePluginServer_VerifiesOwnership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Ownership Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "ownership-toolset")
	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("My Server"),
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
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("My Server"),
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

func TestPluginsService_SetPluginAssignments_NormalizesAndDeduplicatesPrincipalURNs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Dedupe Assignment Test"})
	require.NoError(t, err)

	result, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID: plugin.ID,
		PrincipalUrns: []string{
			"email:Dev@Acme.Corp",
			"email:dev@acme.corp",
			"role:engineering",
			"role:engineering",
			"*",
			"*",
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Assignments, 3)
	require.Equal(t, "email:dev@acme.corp", result.Assignments[0].PrincipalUrn)
	require.Equal(t, "role:engineering", result.Assignments[1].PrincipalUrn)
	require.Equal(t, "*", result.Assignments[2].PrincipalUrn)

	fetched, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Len(t, fetched.Assignments, 3)
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

func TestPluginsService_GetPublishStatus_NotConfigured(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	result, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.False(t, result.Configured)
	require.False(t, result.Connected)
	require.Nil(t, result.RepoOwner)
	require.Nil(t, result.RepoName)
	require.Nil(t, result.RepoURL)
	require.Nil(t, result.MarketplaceURL)
}

func TestPluginsService_PublishPlugins_NotConfigured(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestPluginsService_GetPublishStatus_Configured(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	result, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.True(t, result.Configured)
	require.False(t, result.Connected)
	// Freshness is only meaningful for a connected project.
	require.Nil(t, result.UpToDate)
	require.Nil(t, result.LastPublishedAt)
}

func TestPluginsService_PublishPlugins_HappyPath(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Publish Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "publish-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Test Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	result, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Contains(t, result.RepoURL, "test-org")
	require.True(t, mock.createRepoCalled)
	require.True(t, mock.pushFilesCalled)
	require.NotEmpty(t, mock.lastPushedFiles)

	status, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.True(t, status.Connected)
	require.NotNil(t, status.RepoURL)
	// Publish auto-mints a marketplace token, so the URL must be present
	// and shaped like <server-url>/marketplace/<token>.git.
	require.NotNil(t, status.MarketplaceURL)
	require.Contains(t, *status.MarketplaceURL, "/marketplace/")
	require.Contains(t, *status.MarketplaceURL, ".git")

	// A just-published project is up to date, and the last-published timestamp
	// is surfaced from the connection row.
	require.NotNil(t, status.UpToDate)
	require.True(t, *status.UpToDate)
	require.NotNil(t, status.LastPublishedAt)

	// The observability plugin slugs must name plugins that actually exist in
	// the published marketplace — install UIs build `<plugin>@<marketplace>`
	// strings from them.
	require.NotNil(t, status.ClaudeObservabilityPlugin)
	require.NotNil(t, status.CodexObservabilityPlugin)
	claudePublished := false
	codexPublished := false
	for path := range mock.lastPushedFiles {
		claudePublished = claudePublished || strings.HasPrefix(path, *status.ClaudeObservabilityPlugin+"/")
		codexPublished = codexPublished || strings.HasPrefix(path, *status.CodexObservabilityPlugin+"/")
	}
	require.True(t, claudePublished,
		"claude observability plugin slug %q not found among published files", *status.ClaudeObservabilityPlugin)
	require.True(t, codexPublished,
		"codex observability plugin slug %q not found among published files", *status.CodexObservabilityPlugin)
}

// Reproduces the plugin_github_connections_installation_repo_key conflict:
// project A publishes, gets soft-deleted (freeing its slug under the
// partial projects_organization_id_slug_key index), and project B reuses
// that slug — so both compute the identical repo_owner/repo_name. Since
// soft-deletes never clean up plugin_github_connections, A's row is still
// there when B tries to upsert its own. Publishing as B must succeed by
// reclaiming A's stale row in band, not surface ErrGitHubRepoConflict.
func TestPluginsService_PublishPlugins_ReclaimsStaleConnectionFromDeletedProject(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectAID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	projectA, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, projectAID)
	require.NoError(t, err)
	sharedSlug := projectA.Slug

	pluginA, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Project A Plugin"})
	require.NoError(t, err)
	toolsetA := createTestToolset(t, ctx, ti.conn, "reclaim-toolset-a")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    pluginA.ID,
		ToolsetID:   conv.PtrEmpty(toolsetA.ID.String()),
		DisplayName: conv.PtrEmpty("Server A"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, projectAID)
	require.NoError(t, err)

	projectB, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           sharedSlug,
		Slug:           sharedSlug,
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	authCtx.ProjectID = &projectB.ID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	pluginB, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Project B Plugin"})
	require.NoError(t, err)
	toolsetB := createTestToolset(t, ctx, ti.conn, "reclaim-toolset-b")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    pluginB.ID,
		ToolsetID:   conv.PtrEmpty(toolsetB.ID.String()),
		DisplayName: conv.PtrEmpty("Server B"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	result, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.Contains(t, result.RepoURL, sharedSlug)

	conn, err := pluginsrepo.New(ti.conn).GetGitHubConnection(ctx, projectB.ID)
	require.NoError(t, err)
	require.Equal(t, projectB.ID, conn.ProjectID)
}

// After a publish, mutating the plugin set (here: adding another server) must
// flip the project's published freshness to out-of-date, since the live
// fingerprint no longer matches what was last pushed to GitHub.
func TestPluginsService_GetPublishStatus_StaleAfterEdit(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Freshness Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "freshness-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("First Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	status, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.True(t, *status.UpToDate)

	// Mutate the published plugin set without re-publishing.
	secondToolset := createTestToolset(t, ctx, ti.conn, "freshness-toolset-2")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(secondToolset.ID.String()),
		DisplayName: conv.PtrEmpty("Second Server"),
		Policy:      "required",
		SortOrder:   1,
	})
	require.NoError(t, err)

	status, err = ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.False(t, *status.UpToDate)
	// The timestamp still reflects the prior publish — editing doesn't publish.
	require.NotNil(t, status.LastPublishedAt)
}

// A Remote MCP-backed (mcp_server) plugin server is emitted into the generated
// bundle as an HTTP server pointing at its resolved endpoint URL, with no
// static Authorization header — auth is handled at the HTTP layer via the
// mcp_server's user session issuer (OAuth).
func TestPluginsService_PublishPlugins_McpServerBacked(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Remote Publish"})
	require.NoError(t, err)

	mcpServer := createTestMcpServer(t, ctx, ti.conn, "Remote API", mcpservers.VisibilityPublic)
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		McpServerID: conv.PtrEmpty(mcpServer.idStr),
		DisplayName: conv.PtrEmpty("Remote API"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	claudeMCP := mock.lastPushedFiles["remote-publish/.mcp.json"]
	require.NotNil(t, claudeMCP)

	var claudeConfig struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(claudeMCP, &claudeConfig))

	server, ok := claudeConfig.MCPServers["Remote API"]
	require.True(t, ok)
	require.Equal(t, "http", server.Type)
	require.Equal(t, "https://app.getgram.ai/mcp/"+mcpServer.endpointSlug, server.URL)
	// No static auth header for OAuth (mcp_server-backed) remotes.
	require.Empty(t, server.Headers["Authorization"])
	// And no Gram API key is baked in for a Remote MCP-backed server.
	require.NotContains(t, string(claudeMCP), "gram_local_")
}

func TestPluginsService_PublishPlugins_WithCollaborators(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Collab Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "collab-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Collab Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{
		GithubUsernames: []string{"octocat", "hubot", "monalisa"},
	})
	require.NoError(t, err)
	require.True(t, mock.addCollaboratorCalled)
	require.Equal(t, []string{"octocat", "hubot", "monalisa"}, mock.collaborators)
}

func TestPluginsService_PublishPlugins_CreatesAPIKeyWithCorrectScope(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Key Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "key-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Key Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Verify two keys were created — one consumer-scoped (MCP) and one
	// hooks-scoped (observability plugin).
	keys, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	var mcpKey, hooksKey *keysrepo.ApiKey
	for i := range keys {
		switch {
		case strings.HasPrefix(keys[i].Name, "plugins-mcp-"):
			mcpKey = &keys[i]
		case strings.HasPrefix(keys[i].Name, "plugins-hooks-"):
			hooksKey = &keys[i]
		}
	}
	require.NotNil(t, mcpKey, "expected a plugins-mcp-* API key")
	require.Contains(t, mcpKey.Scopes, "consumer")
	require.True(t, strings.HasPrefix(mcpKey.KeyPrefix, "gram_local_"))

	require.NotNil(t, hooksKey, "expected a plugins-hooks-* API key")
	require.Contains(t, hooksKey.Scopes, "hooks")
	require.True(t, strings.HasPrefix(hooksKey.KeyPrefix, "gram_local_"))

	// Verify the MCP key is injected into the pushed MCP config.
	mcpJSON := mock.lastPushedFiles["key-test/.mcp.json"]
	require.NotNil(t, mcpJSON)
	require.Contains(t, string(mcpJSON), "gram_local_")
	require.NotContains(t, string(mcpJSON), "user_config")
}

func TestPluginsService_PublishPlugins_RePublishCreatesAdditionalKey(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Republish Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "republish-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Republish Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	// Publish twice.
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Count plugin keys — each publish creates two keys (mcp + hooks).
	keys, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	var mcpCount, hooksCount int
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k.Name, "plugins-mcp-"):
			mcpCount++
		case strings.HasPrefix(k.Name, "plugins-hooks-"):
			hooksCount++
		}
	}
	// A human re-publish always refreshes the MCP component, minting a fresh MCP
	// key each time (prior ones are not revoked — a future improvement should
	// revoke first). The hooks component is decoupled: it is carried verbatim from
	// the existing repo unless hooksGeneratorVersion bumps, so re-publishing does
	// NOT mint a second hooks key.
	require.Equal(t, 2, mcpCount, "expected 2 mcp keys after 2 publishes")
	require.Equal(t, 1, hooksCount, "expected hooks key to be reused (carried) across republishes")
}

// PublishPlugins must not persist the API key (or audit log entry, or
// github connection) when GitHub publishing fails. Otherwise every failed
// publish leaks a valid consumer credential.
func TestPluginsService_PublishPlugins_NoOrphanedKeyOnGitHubFailure(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{
		createRepoErr: errors.New("simulated github outage"),
	}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Will Fail"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "fail-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.Error(t, err, "publish must fail when GitHub does")

	// No plugins-* API key should have been persisted.
	keys, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	for _, k := range keys {
		require.False(t, strings.HasPrefix(k.Name, "plugins-"),
			"orphaned plugin api key %q persisted despite github failure", k.Name)
	}
}

func TestPluginsService_PublishPlugins_PublicToolsetEnvConfigs(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Public Test"})
	require.NoError(t, err)

	// Create a toolset and make it public.
	toolset := createTestToolset(t, ctx, ti.conn, "public-toolset")
	err = toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	// Create MCP metadata + environment config for the public toolset.
	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{Valid: false},
		ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID:                    uuid.NullUUID{Valid: false},
		Instructions:              pgtype.Text{Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{Valid: false},
		InstallationOverrideUrl:   pgtype.Text{Valid: false},
	})
	require.NoError(t, err)

	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "ANALYTICS_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "Authorization", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Public Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Verify the Cursor config uses ${env:ANALYTICS_API_KEY} for the public server.
	cursorMCP := mock.lastPushedFiles["cursor-plugins/public-test-cursor/mcp.json"]
	require.NotNil(t, cursorMCP)

	var cursorConfig struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	err = json.Unmarshal(cursorMCP, &cursorConfig)
	require.NoError(t, err)

	server, ok := cursorConfig.MCPServers["Public Server"]
	require.True(t, ok)
	require.Equal(t, "${env:ANALYTICS_API_KEY}", server.Headers["Authorization"])

	// Verify the Claude config uses ${user_config.ANALYTICS_API_KEY}.
	claudeMCP := mock.lastPushedFiles["public-test/.mcp.json"]
	require.NotNil(t, claudeMCP)
	require.Contains(t, string(claudeMCP), "${user_config.ANALYTICS_API_KEY}")

	// Verify NO Gram API key is injected for public servers.
	require.NotContains(t, string(cursorMCP), "gram_local_")
}

// User-provided env configs without a HeaderDisplayName must be skipped —
// substituting the variable name as the HTTP header would produce a broken
// MCP config. Only configs with an explicit header name should be emitted.
func TestPluginsService_PublishPlugins_SkipsUserEnvConfigsWithoutHeaderName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Headerless"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "headerless-toolset")
	err = toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{Valid: false},
		ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID:                    uuid.NullUUID{Valid: false},
		Instructions:              pgtype.Text{Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{Valid: false},
		InstallationOverrideUrl:   pgtype.Text{Valid: false},
	})
	require.NoError(t, err)

	// Two user-provided env configs: one with header name, one without.
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "HAS_HEADER",
		HeaderDisplayName: pgtype.Text{String: "X-Has-Header", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "NO_HEADER",
		HeaderDisplayName: pgtype.Text{Valid: false},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Headerless Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	cursorMCP := mock.lastPushedFiles["cursor-plugins/headerless-cursor/mcp.json"]
	require.NotNil(t, cursorMCP)

	var cursorConfig struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(cursorMCP, &cursorConfig))

	server, ok := cursorConfig.MCPServers["Headerless Server"]
	require.True(t, ok)

	// HAS_HEADER survived with its proper header name.
	require.Equal(t, "${env:HAS_HEADER}", server.Headers["X-Has-Header"])

	// NO_HEADER must not leak through as either a header key or a value.
	require.NotContains(t, server.Headers, "NO_HEADER")
	for k, v := range server.Headers {
		require.NotContains(t, v, "NO_HEADER", "NO_HEADER variable leaked into header %q", k)
	}
}

// AddPluginServer rejects toolsets without mcp_enabled, but mcp can be
// disabled later without removing the persisted mcp_slug. The publish path
// must filter those out so generated configs don't reference dead URLs.
func TestPluginsService_PublishPlugins_SkipsDisabledMCPToolsets(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Mixed"})
	require.NoError(t, err)

	enabled := createTestToolset(t, ctx, ti.conn, "enabled-toolset")
	disabled := createTestToolset(t, ctx, ti.conn, "disabled-toolset")

	for _, ts := range []toolsetsrepo.Toolset{enabled, disabled} {
		_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
			PluginID:    plugin.ID,
			ToolsetID:   conv.PtrEmpty(ts.ID.String()),
			DisplayName: conv.PtrEmpty("Server " + ts.Name),
			Policy:      "required",
			SortOrder:   0,
		})
		require.NoError(t, err)
	}

	// Disable MCP after the server was added (slug stays persisted).
	err = toolsetsrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{
		McpEnabled: false,
		ID:         disabled.ID,
		ProjectID:  disabled.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	cursorMCP := mock.lastPushedFiles["cursor-plugins/mixed-cursor/mcp.json"]
	require.NotNil(t, cursorMCP)

	var cursorConfig struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(cursorMCP, &cursorConfig))

	require.Contains(t, cursorConfig.MCPServers, "Server enabled-toolset", "enabled toolset should appear in published config")
	require.NotContains(t, cursorConfig.MCPServers, "Server disabled-toolset", "disabled toolset must be filtered out by ListPluginsWithServersForProject")
}

// A public toolset with no mcp_metadata row should publish cleanly. The
// metadata row is created by an explicit UpsertMetadata call, not auto-
// created when a toolset is made public, so this is a real production state.
func TestPluginsService_PublishPlugins_PublicToolsetWithoutMetadata(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "No Meta"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "public-no-meta")
	err = toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("No Meta Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
}

// PublishPlugins always emits a per-org observability plugin containing
// the bootstrapper, relay config, and provider hook manifest.
func TestPluginsService_PublishPlugins_EmitsObservabilityPlugin(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Observability Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "observability-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	claudeObservability, cursorObservability := orgObservabilitySlugs(t, ctx, ti)

	// Both Claude and Cursor observability plugins must be present, with
	// hooks living under hooks/ per the Claude Code plugins reference. Files
	// at the plugin root register the plugin but never wire the hooks up.
	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/.claude-plugin/plugin.json"], "claude observability plugin.json missing")
	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/hooks/hooks.json"], "claude observability hooks/hooks.json missing")
	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/hooks/bootstrap.sh"], "claude observability bootstrap.sh missing")
	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/speakeasy.json"], "claude observability speakeasy.json missing")

	require.NotNil(t, mock.lastPushedFiles["cursor-plugins/"+cursorObservability+"/.cursor-plugin/plugin.json"], "cursor observability plugin.json missing")
	require.NotNil(t, mock.lastPushedFiles["cursor-plugins/"+cursorObservability+"/hooks/hooks.json"], "cursor observability hooks/hooks.json missing")
	require.NotNil(t, mock.lastPushedFiles["cursor-plugins/"+cursorObservability+"/hooks/bootstrap.sh"], "cursor observability bootstrap.sh missing")
	require.NotNil(t, mock.lastPushedFiles["cursor-plugins/"+cursorObservability+"/speakeasy.json"], "cursor observability speakeasy.json missing")
}

// PublishPlugins must succeed when the org has no custom plugins — the
// observability plugin alone is enough to ship a marketplace, since it's
// always emitted on publish.
func TestPluginsService_PublishPlugins_ObservabilityOnly(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	_, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	claudeObservability, cursorObservability := orgObservabilitySlugs(t, ctx, ti)

	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/hooks/bootstrap.sh"], "claude observability bootstrap.sh missing")
	require.NotNil(t, mock.lastPushedFiles["cursor-plugins/"+cursorObservability+"/hooks/bootstrap.sh"], "cursor observability bootstrap.sh missing")

	for _, p := range []struct {
		path     string
		expected string
	}{
		{".claude-plugin/marketplace.json", claudeObservability},
		{".cursor-plugin/marketplace.json", cursorObservability},
	} {
		raw := mock.lastPushedFiles[p.path]
		require.NotNil(t, raw, p.path+" missing")
		var market struct {
			Plugins []struct {
				Name string `json:"name"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal(raw, &market))
		require.Len(t, market.Plugins, 1, "expected only observability plugin in %s", p.path)
		require.Equal(t, p.expected, market.Plugins[0].Name, "%s", p.path)
	}
}

// The observability config contains the freshly-minted hooks-scoped API key,
// while provider commands and bootstrappers remain secret-free.
func TestPluginsService_PublishPlugins_ObservabilityConfigContainsAPIKey(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Hook Key"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "hookkey-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("S"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	keys, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var hooksKeyPrefix string
	for _, k := range keys {
		if strings.HasPrefix(k.Name, "plugins-hooks-") {
			hooksKeyPrefix = k.KeyPrefix
			break
		}
	}
	require.NotEmpty(t, hooksKeyPrefix, "expected a plugins-hooks-* API key")

	claudeObservability, cursorObservability := orgObservabilitySlugs(t, ctx, ti)
	// Relay configs embed the publish-time hooks key as the org-wide fallback:
	// per-user browser login still takes precedence when cached, but a machine
	// with no personal credentials sends through the baked key instead of
	// degrading to the unauthenticated pass-through.
	for _, root := range []string{claudeObservability, "cursor-plugins/" + cursorObservability} {
		config := string(mock.lastPushedFiles[root+"/speakeasy.json"])
		require.NotEmpty(t, config, root+"/speakeasy.json missing")
		require.Contains(t, config, hooksKeyPrefix)
		require.NotContains(t, config, "plugins-mcp-", "%s leaked the MCP key", root)
		require.NotContains(t, string(mock.lastPushedFiles[root+"/hooks/bootstrap.sh"]), hooksKeyPrefix)
		require.NotContains(t, string(mock.lastPushedFiles[root+"/hooks/hooks.json"]), hooksKeyPrefix)
	}
}

// The observability plugin must appear FIRST in each platform's marketplace
// listing so team admins see it before any feature plugins.
func TestPluginsService_PublishPlugins_ObservabilityListedFirstInMarketplace(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	// Create two feature plugins so we can verify order, not just presence.
	for _, name := range []string{"Alpha", "Bravo"} {
		p, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name})
		require.NoError(t, err)
		ts := createTestToolset(t, ctx, ti.conn, "ts-"+name)
		_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
			PluginID:    p.ID,
			ToolsetID:   conv.PtrEmpty(ts.ID.String()),
			DisplayName: conv.PtrEmpty("Server " + name),
			Policy:      "required",
			SortOrder:   0,
		})
		require.NoError(t, err)
	}

	_, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	claudeObservability, cursorObservability := orgObservabilitySlugs(t, ctx, ti)
	for _, p := range []struct {
		path        string
		expectFirst string
	}{
		{".claude-plugin/marketplace.json", claudeObservability},
		{".cursor-plugin/marketplace.json", cursorObservability},
	} {
		raw := mock.lastPushedFiles[p.path]
		require.NotNil(t, raw, p.path+" missing")
		var market struct {
			Plugins []struct {
				Name string `json:"name"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal(raw, &market))
		require.GreaterOrEqual(t, len(market.Plugins), 3, "expected observability + 2 features in %s", p.path)
		require.Equal(t, p.expectFirst, market.Plugins[0].Name, "observability plugin must be first in %s", p.path)
	}
}
func TestPluginsService_PublishPlugins_CodexPackageHappyPath(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Codex Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "codex-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Codex Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Per-plugin manifest exists at the expected path.
	manifest := mock.lastPushedFiles["codex-test-codex/.codex-plugin/plugin.json"]
	require.NotNil(t, manifest, "codex plugin.json missing")

	var pluginManifest struct {
		Name       string `json:"name"`
		MCPServers string `json:"mcpServers"`
		Interface  struct {
			DisplayName string `json:"displayName"`
		} `json:"interface"`
	}
	require.NoError(t, json.Unmarshal(manifest, &pluginManifest))
	require.Equal(t, "codex-test-codex", pluginManifest.Name)
	require.Equal(t, "./.mcp.json", pluginManifest.MCPServers)
	require.Equal(t, "Codex Test", pluginManifest.Interface.DisplayName)

	// Per-plugin .mcp.json has the server with a baked-in bearer token.
	mcpFile := mock.lastPushedFiles["codex-test-codex/.mcp.json"]
	require.NotNil(t, mcpFile, "codex .mcp.json missing")

	var mcpConfig struct {
		MCPServers map[string]struct {
			URL         string            `json:"url"`
			HTTPHeaders map[string]string `json:"http_headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mcpFile, &mcpConfig))

	// The key is the display name sanitized to Codex's ^[a-zA-Z0-9_-]+$
	// server-name pattern — "Codex Server" would fail MCP client startup.
	server, ok := mcpConfig.MCPServers["Codex_Server"]
	require.True(t, ok)
	require.Contains(t, server.URL, "/mcp/")
	require.Contains(t, server.HTTPHeaders["Authorization"], "Bearer gram_local_")

	// Repo-root marketplace lists the codex plugin.
	mp := mock.lastPushedFiles[".agents/plugins/marketplace.json"]
	require.NotNil(t, mp, "codex marketplace.json missing")

	var market struct {
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			} `json:"source"`
			Policy struct {
				Installation   string `json:"installation"`
				Authentication string `json:"authentication"`
			} `json:"policy"`
		} `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(mp, &market))
	// Observability plugin ships first, then the feature plugin.
	require.Len(t, market.Plugins, 2)
	require.Contains(t, market.Plugins[0].Name, "observability-codex", "observability plugin must be first")
	require.Equal(t, "local", market.Plugins[0].Source.Source)
	require.Equal(t, "INSTALLED_BY_DEFAULT", market.Plugins[0].Policy.Installation)
	require.Equal(t, "ON_USE", market.Plugins[0].Policy.Authentication)
	require.Equal(t, "codex-test-codex", market.Plugins[1].Name)
	require.Equal(t, "local", market.Plugins[1].Source.Source)
	require.Equal(t, "./codex-test-codex", market.Plugins[1].Source.Path)
	require.Equal(t, "AVAILABLE", market.Plugins[1].Policy.Installation)
	// Private server + baked API key: nothing to prompt for, so install-silent.
	require.Equal(t, "ON_USE", market.Plugins[1].Policy.Authentication)
}

// Public servers map user-provided env configs to Codex's env_http_headers,
// so the user's shell environment populates the header at runtime without
// needing a separate prompt mechanism.
func TestPluginsService_PublishPlugins_CodexPublicToolsetEnvHeaders(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Codex Public"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "codex-public-toolset")
	err = toolsetsrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{Valid: false},
		ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID:                    uuid.NullUUID{Valid: false},
		Instructions:              pgtype.Text{Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{Valid: false},
		InstallationOverrideUrl:   pgtype.Text{Valid: false},
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "ANALYTICS_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "Authorization", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)

	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Public Codex Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	mcpFile := mock.lastPushedFiles["codex-public-codex/.mcp.json"]
	require.NotNil(t, mcpFile)

	var mcpConfig struct {
		MCPServers map[string]struct {
			URL            string            `json:"url"`
			EnvHTTPHeaders map[string]string `json:"env_http_headers"`
			HTTPHeaders    map[string]string `json:"http_headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mcpFile, &mcpConfig))

	server, ok := mcpConfig.MCPServers["Public_Codex_Server"]
	require.True(t, ok)
	// Codex resolves env_http_headers["Authorization"] = "ANALYTICS_API_KEY"
	// by reading the ANALYTICS_API_KEY env var at runtime and using its
	// value as the Authorization header. No baked-in bearer token.
	require.Equal(t, "ANALYTICS_API_KEY", server.EnvHTTPHeaders["Authorization"])
	require.Empty(t, server.HTTPHeaders, "public servers must not bake in static headers")
}

// Disabled MCP toolsets must be filtered from Codex output too — same
// guarantee as Claude/Cursor since the underlying query is shared.
func TestPluginsService_PublishPlugins_CodexSkipsDisabledMCPToolsets(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Codex Mixed"})
	require.NoError(t, err)

	enabled := createTestToolset(t, ctx, ti.conn, "codex-enabled")
	disabled := createTestToolset(t, ctx, ti.conn, "codex-disabled")

	for _, ts := range []toolsetsrepo.Toolset{enabled, disabled} {
		_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
			PluginID:    plugin.ID,
			ToolsetID:   conv.PtrEmpty(ts.ID.String()),
			DisplayName: conv.PtrEmpty("Server " + ts.Name),
			Policy:      "required",
			SortOrder:   0,
		})
		require.NoError(t, err)
	}

	err = toolsetsrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{
		McpEnabled: false,
		ID:         disabled.ID,
		ProjectID:  disabled.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	mcpFile := mock.lastPushedFiles["codex-mixed-codex/.mcp.json"]
	require.NotNil(t, mcpFile)

	var mcpConfig struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(mcpFile, &mcpConfig))

	require.Contains(t, mcpConfig.MCPServers, "Server_codex-enabled")
	require.NotContains(t, mcpConfig.MCPServers, "Server_codex-disabled")
}

// PublishProject with SkipIfUnchanged set re-publishes the first time (no
// stored fingerprint), skips when nothing changed, and re-publishes again once
// the plugin set changes.
func TestPluginsService_PublishProject_SkipsWhenUnchanged(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Skip Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "skip-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Skip Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	input := plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	}

	// First publish: no fingerprint on record yet, so it must publish.
	first, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.False(t, first.Skipped)
	require.True(t, mock.pushFilesCalled)

	// Second publish, nothing changed: the fingerprint matches, so it skips
	// without touching GitHub or minting keys.
	mock.pushFilesCalled = false
	mock.createRepoCalled = false
	mock.addCollaboratorCalled = false
	second, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.True(t, second.Skipped, "unchanged project must be skipped")
	require.False(t, mock.pushFilesCalled, "skipped publish must not push to GitHub")
	require.False(t, mock.createRepoCalled, "skipped publish must not call CreateRepo")
	require.False(t, mock.addCollaboratorCalled, "skipped publish must not add collaborators")

	// Changing the plugin set changes the fingerprint, forcing a re-publish.
	toolset2 := createTestToolset(t, ctx, ti.conn, "skip-toolset-2")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset2.ID.String()),
		DisplayName: conv.PtrEmpty("Skip Server 2"),
		Policy:      "optional",
		SortOrder:   1,
	})
	require.NoError(t, err)

	third, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.False(t, third.Skipped, "changed plugin set must re-publish")
	require.True(t, mock.pushFilesCalled)
}

// hooksFilesOf returns the observability (hooks) plugin files from a pushed file
// map. The root speakeasy.json carries settings and the hooks/ directory carries
// provider commands and bootstrappers, so both are needed to detect regeneration.
func hooksFilesOf(files map[string][]byte) map[string]string {
	out := make(map[string]string)
	for p, c := range files {
		if strings.Contains(p, "/hooks/") || (strings.Contains(p, "observability") && strings.HasSuffix(p, "/speakeasy.json")) {
			out[p] = string(c)
		}
	}
	return out
}

// observabilityManifestVersion extracts the version stamped into the pushed
// Claude observability plugin.json — the value platform marketplaces compare
// to decide whether installed copies refresh.
func observabilityManifestVersion(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for p, c := range files {
		if strings.Contains(p, "observability") && strings.HasSuffix(p, ".claude-plugin/plugin.json") {
			var meta struct {
				Version string `json:"version"`
			}
			require.NoError(t, json.Unmarshal(c, &meta), "parse %s", p)
			require.NotEmpty(t, meta.Version)
			return meta.Version
		}
	}
	t.Fatal("no claude observability plugin.json among pushed files")
	return ""
}

// An MCP-only republish must carry the observability (hooks) plugin verbatim —
// the whole point of decoupling the two components. The hooks files (including
// their embedded hooks API key) must be byte-identical across a publish driven
// solely by an MCP content change.
func TestPluginsService_PublishProject_MCPChangeCarriesHooksVerbatim(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Carry Hooks"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "carry-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Carry Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	input := plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	}

	first, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.False(t, first.Skipped)

	hooksBefore := hooksFilesOf(mock.lastPushedFiles)
	require.NotEmpty(t, hooksBefore, "first publish must emit hooks files")

	// Change the plugin set — an MCP-only change; hooksGeneratorVersion is untouched.
	toolset2 := createTestToolset(t, ctx, ti.conn, "carry-toolset-2")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset2.ID.String()),
		DisplayName: conv.PtrEmpty("Carry Server 2"),
		Policy:      "optional",
		SortOrder:   1,
	})
	require.NoError(t, err)

	second, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.False(t, second.Skipped, "MCP change must republish")
	require.True(t, mock.getRepoFilesCalled, "carrying hooks requires fetching the existing repo")

	hooksAfter := hooksFilesOf(mock.lastPushedFiles)
	require.Equal(t, hooksBefore, hooksAfter, "hooks subtree must be carried verbatim across an MCP-only publish")
}

// phasedRolloutFixture creates a published project and rewinds its stored hooks
// version to "0", leaving a pending hooks bump for the phased rollout to gate.
// It returns the baseline hooks files so callers can assert carry-vs-regenerate.
func phasedRolloutFixture(t *testing.T, ctx context.Context, ti *testInstance, mock *mockGitHubPublisher, name string) (pluginID string, hooksBaseline map[string]string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, name+"-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty(name + " Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	// Baseline publish (non-phased) records the current hooks version + MCP
	// fingerprints, then we rewind the stored hooks version so a bump is pending.
	_, err = ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "baseline",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)

	hooksBaseline = hooksFilesOf(mock.lastPushedFiles)
	require.NotEmpty(t, hooksBaseline, "baseline publish must emit hooks files")

	rewindPublishedHooksVersion(t, ctx, ti.conn, *authCtx.ProjectID, "0")

	return plugin.ID, hooksBaseline
}

// A phase-gated org that is NOT in the rollout must not receive a pending hooks
// bump: with no MCP change, the publish skips entirely and the stored hooks
// version is left untouched (the org stays on what it already has).
func TestPluginsService_PublishProject_PhasedRollout_NonEligibleBlocksHooksBump(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	phasedRolloutFixture(t, ctx, ti, mock, "Phased NonEligible")

	// Empty provider → no clearance payload → org is not in the rollout phase.
	pub := newTestPluginPublisher(t, ti, mock, &feature.InMemory{})

	mock.pushFilesCalled = false
	mock.getRepoFilesCalled = false
	res, err := pub.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "phased",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.True(t, res.Skipped, "non-eligible org with a pending hooks bump and no content change must skip")
	require.False(t, mock.pushFilesCalled, "a gated hooks bump must not push to GitHub")
	require.Equal(t, "0", publishedHooksVersion(t, ctx, ti.conn, *authCtx.ProjectID), "gated org keeps its old hooks version")
}

// An org cleared by the FlagHooksRollout payload rolls the pending hooks bump
// forward: hooks are regenerated (fresh key, so the subtree differs) and the
// stored version advances off the rewound value.
func TestPluginsService_PublishProject_PhasedRollout_EligibleGetsHooksBump(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	phasedRolloutFixture(t, ctx, ti, mock, "Phased Eligible")

	orgID := publishOrgID(t, ctx, ti.conn, *authCtx.ProjectID)
	hooksKeysBefore := countPluginHooksKeys(t, ctx, ti.conn, orgID)

	// A pin above any plausible generator version clears this org for the bump.
	features := &feature.InMemory{}
	features.SetFlagPayload(feature.FlagHooksRollout, orgID, []byte(`{"version": 9999}`))
	pub := newTestPluginPublisher(t, ti, mock, features)

	mock.pushFilesCalled = false
	res, err := pub.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "phased",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.False(t, res.Skipped, "eligible org must roll the pending hooks bump forward")
	require.True(t, mock.pushFilesCalled)

	// A regenerated hooks component mints a fresh hooks-scoped key (the hook
	// scripts themselves no longer embed the key, so their bytes are stable).
	require.Equal(t, hooksKeysBefore+1, countPluginHooksKeys(t, ctx, ti.conn, orgID), "eligible org regenerates the hooks subtree with a fresh key")
	require.NotEqual(t, "0", publishedHooksVersion(t, ctx, ti.conn, *authCtx.ProjectID), "eligible org advances its stored hooks version")
}

// MCP content changes must publish regardless of the hooks rollout phase: a
// gated org still gets its MCP update, and its hooks are carried verbatim rather
// than rolled forward.
func TestPluginsService_PublishProject_PhasedRollout_MCPPublishesRegardlessOfPhase(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginID, hooksBefore := phasedRolloutFixture(t, ctx, ti, mock, "Phased MCP")
	orgID := publishOrgID(t, ctx, ti.conn, *authCtx.ProjectID)
	hooksKeysBefore := countPluginHooksKeys(t, ctx, ti.conn, orgID)

	// An MCP content change (new server) while the org remains phase-gated.
	toolset2 := createTestToolset(t, ctx, ti.conn, "phased-mcp-2")
	_, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    pluginID,
		ToolsetID:   conv.PtrEmpty(toolset2.ID.String()),
		DisplayName: conv.PtrEmpty("Phased MCP Server 2"),
		Policy:      "optional",
		SortOrder:   1,
	})
	require.NoError(t, err)

	pub := newTestPluginPublisher(t, ti, mock, &feature.InMemory{})

	mock.pushFilesCalled = false
	mock.getRepoFilesCalled = false
	res, err := pub.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "phased",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.False(t, res.Skipped, "MCP content change must publish even for a phase-gated org")
	require.True(t, mock.pushFilesCalled)
	require.True(t, mock.getRepoFilesCalled, "carrying hooks requires fetching the existing repo")

	hooksAfter := hooksFilesOf(mock.lastPushedFiles)
	require.Equal(t, hooksBefore, hooksAfter, "phase-gated org carries hooks verbatim while MCP publishes")
	require.Equal(t, hooksKeysBefore, countPluginHooksKeys(t, ctx, ti.conn, orgID), "carrying hooks must not mint a new hooks key")
	require.Equal(t, "0", publishedHooksVersion(t, ctx, ti.conn, *authCtx.ProjectID), "an MCP publish must not advance a gated org's hooks version")
}

// Flipping an org-level hooks setting must regenerate the hooks subtree on the
// next publish even though hooksGeneratorVersion is unchanged: the rendered
// scripts bake the setting in, so carrying them verbatim would leave the old
// behavior live until an unrelated generator bump. The persisted
// published_hooks_config is what detects the flip. The org is cleared for the
// current hooks version up front — a non-eligible org would defer the flip
// instead (see hooks_config_test.go for the deferral path).
func TestPluginsService_PublishProject_RegeneratesHooksOnBrowserLoginFlip(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Pin above any real generator version so the flip regenerates rather than
	// defers under the rollout gate.
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Flip Hooks"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "flip-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Flip Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	input := plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	}

	first, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.False(t, first.Skipped)
	hooksBefore := hooksFilesOf(mock.lastPushedFiles)
	require.NotEmpty(t, hooksBefore)
	versionBefore := observabilityManifestVersion(t, mock.lastPushedFiles)

	_, pfErr := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureHooksBrowserLogin),
	})
	require.NoError(t, pfErr)

	second, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.False(t, second.Skipped, "a hooks settings flip must republish")
	hooksAfter := hooksFilesOf(mock.lastPushedFiles)
	require.NotEqual(t, hooksBefore, hooksAfter,
		"hooks subtree must be regenerated, not carried, after a settings flip")
	require.NotEqual(t, versionBefore, observabilityManifestVersion(t, mock.lastPushedFiles),
		"the observability plugin.json version must move on a settings flip, or installed copies never refresh")

	// The regenerated config is persisted, so the next unchanged rollout skips.
	third, err := ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.True(t, third.Skipped, "republishing with the same settings must skip again")
}

// A dashboard publish (PublishPlugins, which never skips) must still record the
// fingerprint, so a subsequent automated rollout sees the project as unchanged.
func TestPluginsService_PublishProject_SkipsAfterDashboardPublish(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Dashboard Then Rollout"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "dashboard-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Dashboard Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	require.True(t, mock.pushFilesCalled)

	mock.pushFilesCalled = false
	result, err := ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.True(t, result.Skipped, "rollout must skip a project the dashboard just published unchanged")
	require.False(t, mock.pushFilesCalled)
}
