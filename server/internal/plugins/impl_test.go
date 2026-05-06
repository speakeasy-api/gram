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
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// mockGitHubPublisher records calls for testing. Set the *Err fields to
// simulate GitHub-side failures.
type mockGitHubPublisher struct {
	createRepoCalled      bool
	pushFilesCalled       bool
	addCollaboratorCalled bool
	lastPushedFiles       map[string][]byte
	createRepoErr         error
	pushFilesErr          error
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

func (m *mockGitHubPublisher) AddCollaborator(_ context.Context, _ int64, _, _, _, _ string) error {
	m.addCollaboratorCalled = true
	return nil
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Test Server",
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
	// and shaped like <server-url>/marketplace/m/<token>/marketplace.json.
	require.NotNil(t, status.MarketplaceURL)
	require.Contains(t, *status.MarketplaceURL, "/marketplace/m/")
	require.Contains(t, *status.MarketplaceURL, "/marketplace.json")
}

func TestPluginsService_PublishPlugins_WithCollaborator(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Collab Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "collab-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Collab Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	username := "octocat"
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{
		GithubUsername: &username,
	})
	require.NoError(t, err)
	require.True(t, mock.addCollaboratorCalled)
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Key Server",
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Republish Server",
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
	// Documents the current behavior: each publish mints fresh keys without
	// revoking prior ones. A future improvement should revoke first.
	require.Equal(t, 2, mcpCount, "expected 2 mcp keys after 2 publishes")
	require.Equal(t, 2, hooksCount, "expected 2 hooks keys after 2 publishes")
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Server",
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
		ToolsetID:                 toolset.ID,
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Public Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Verify the Cursor config uses ${env:ANALYTICS_API_KEY} for the public server.
	cursorMCP := mock.lastPushedFiles["public-test-cursor/mcp.json"]
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
		ToolsetID:                 toolset.ID,
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Headerless Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	cursorMCP := mock.lastPushedFiles["headerless-cursor/mcp.json"]
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
			ToolsetID:   ts.ID.String(),
			DisplayName: "Server " + ts.Name,
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

	cursorMCP := mock.lastPushedFiles["mixed-cursor/mcp.json"]
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "No Meta Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
}

// PublishPlugins always emits a per-org observability plugin containing
// Gram hooks. The hook script bakes in the hooks-scoped API key so team
// members don't need to configure credentials per-machine.
func TestPluginsService_PublishPlugins_EmitsObservabilityPlugin(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Observability Test"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "observability-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Server",
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
	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/hooks/hook.sh"], "claude observability hooks/hook.sh missing")

	require.NotNil(t, mock.lastPushedFiles[cursorObservability+"/.cursor-plugin/plugin.json"], "cursor observability plugin.json missing")
	require.NotNil(t, mock.lastPushedFiles[cursorObservability+"/hooks/hooks.json"], "cursor observability hooks/hooks.json missing")
	require.NotNil(t, mock.lastPushedFiles[cursorObservability+"/hooks/hook.sh"], "cursor observability hooks/hook.sh missing")
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

	require.NotNil(t, mock.lastPushedFiles[claudeObservability+"/hooks/hook.sh"], "claude observability hooks/hook.sh missing")
	require.NotNil(t, mock.lastPushedFiles[cursorObservability+"/hooks/hook.sh"], "cursor observability hooks/hook.sh missing")

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

// The observability hook script must contain the freshly-minted hooks-scoped
// API key (Bearer-token form) so team members can install the plugin without
// any per-machine credential configuration.
func TestPluginsService_PublishPlugins_ObservabilityHookScriptContainsAPIKey(t *testing.T) {
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "S",
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
	// Both endpoints accept Gram-Key (Cursor requires it via Security; Claude
	// accepts it as an optional header for plugin-driven attribution).
	for _, path := range []string{claudeObservability + "/hooks/hook.sh", cursorObservability + "/hooks/hook.sh"} {
		script := string(mock.lastPushedFiles[path])
		require.NotEmpty(t, script, path+" missing")
		require.Contains(t, script, "Gram-Key: "+hooksKeyPrefix, "%s does not embed hooks key in Gram-Key", path)
		// Must NOT contain the MCP key — separate scope, separate concerns.
		require.NotContains(t, script, "plugins-mcp-", "%s leaked the MCP key", path)
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
			ToolsetID:   ts.ID.String(),
			DisplayName: "Server " + name,
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Codex Server",
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

	server, ok := mcpConfig.MCPServers["Codex Server"]
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
	require.Len(t, market.Plugins, 1)
	require.Equal(t, "codex-test-codex", market.Plugins[0].Name)
	require.Equal(t, "local", market.Plugins[0].Source.Source)
	require.Equal(t, "./codex-test-codex", market.Plugins[0].Source.Path)
	require.Equal(t, "AVAILABLE", market.Plugins[0].Policy.Installation)
	// Private server + baked API key: nothing to prompt for, so install-silent.
	require.Equal(t, "ON_USE", market.Plugins[0].Policy.Authentication)
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
		ToolsetID:                 toolset.ID,
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
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Public Codex Server",
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

	server, ok := mcpConfig.MCPServers["Public Codex Server"]
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
			ToolsetID:   ts.ID.String(),
			DisplayName: "Server " + ts.Name,
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

	require.Contains(t, mcpConfig.MCPServers, "Server codex-enabled")
	require.NotContains(t, mcpConfig.MCPServers, "Server codex-disabled")
}
