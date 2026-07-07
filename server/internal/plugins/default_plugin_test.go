package plugins_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestEnsureDefaultPlugin_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	result, err := plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "Default", result.Plugin.Name)
	require.Equal(t, "default", result.Plugin.Slug)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, result.Plugin.IsDefault)
}

func TestEnsureDefaultPlugin_ReturnsExistingWhenPresent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	first, err := plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, first.Created)

	second, err := plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Equal(t, first.Plugin.ID, second.Plugin.ID)
}

func TestEnsureDefaultPlugin_ConflictWithExistingNonDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	// A plugin already occupies the "Default"/"default" name+slug, but isn't
	// marked is_default — a real conflict, not a race with another Ensure
	// call, so it must surface as an error rather than being masked.
	_, err := queries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Default",
		Slug:           "default",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = plugins.EnsureDefaultPlugin(ctx, queries, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.Error(t, err)
}

func TestAttachToDefaultPlugin_DisplayNameCollision_SuffixesName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	first := createTestMcpServer(t, ctx, ti.conn, "Attach Test Server", mcpservers.VisibilityPublic)
	result, err := plugins.AttachToDefaultPlugin(ctx, queries, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		McpServerID:    uuid.NullUUID{UUID: first.id, Valid: true},
		DisplayName:    "Attach Test Server",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// A different mcp_server that derives the same display name must still
	// attach — a name collision is cosmetic and must not fail the caller's
	// surrounding transaction (an endpoint create or a server enable) — so
	// the name is de-conflicted with a numeric suffix instead.
	second := createTestMcpServer(t, ctx, ti.conn, "Attach Test Server 2", mcpservers.VisibilityPublic)
	secondResult, err := plugins.AttachToDefaultPlugin(ctx, queries, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		McpServerID:    uuid.NullUUID{UUID: second.id, Valid: true},
		DisplayName:    "Attach Test Server",
	})
	require.NoError(t, err)
	require.NotNil(t, secondResult)
	require.Equal(t, "Attach Test Server 2", secondResult.Server.DisplayName)

	servers, err := queries.ListPluginServers(ctx, result.PluginID)
	require.NoError(t, err)
	require.Len(t, servers, 2)
}

func TestListPluginPublishCandidates_IncludesNeverPublishedDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	// This project has a Default plugin but has never published — no
	// plugin_github_connections row and no plugins-mcp API key. It must
	// still show up as a candidate (the periodic safety net for a lost
	// initial-publish enqueue), with a 'system' actor fallback.
	_, err := queries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	candidates, err := queries.ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{
		AfterProjectID: uuid.Nil,
		ResultLimit:    100,
	})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, *authCtx.ProjectID, candidates[0].ProjectID)
	require.Equal(t, "system", candidates[0].CreatedByUserID)
}

func TestListPluginPublishCandidates_ExcludesProjectWithoutDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	queries := pluginsrepo.New(ti.conn)

	candidates, err := queries.ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{
		AfterProjectID: uuid.Nil,
		ResultLimit:    100,
	})
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestListPluginPublishCandidates_IncludesConnectedProjectWithoutDefaultPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	// This project already published before the Default-plugin feature
	// shipped (a plugin_github_connections row exists) but has had no new
	// attach activity since, so it never got a Default plugin. It must not
	// silently drop out of the rollout just because is_default became part
	// of the candidate signal.
	_, err := queries.UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:            *authCtx.ProjectID,
		InstallationID:       12345,
		RepoOwner:            "test-org",
		RepoName:             "test-project-plugins",
		MarketplaceToken:     pgtype.Text{String: "test-token", Valid: true},
		PublishedFingerprint: pgtype.Text{String: "test-fingerprint", Valid: true},
	})
	require.NoError(t, err)

	candidates, err := queries.ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{
		AfterProjectID: uuid.Nil,
		ResultLimit:    100,
	})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, *authCtx.ProjectID, candidates[0].ProjectID)
}
