package plugins_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestEnsureDefaultPlugin_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tx := testenv.BeginTx(t, ctx, ti.conn)

	result, err := plugins.EnsureDefaultPlugin(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "Default", result.Plugin.Name)
	require.Equal(t, "default", result.Plugin.Slug)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, result.Plugin.IsDefault)

	// A freshly-created Default plugin in the org's default project (the test's
	// only, and thus oldest, project) is assigned to the org wildcard so it
	// delivers to everyone under agent.getPlugins' per-principal scoping.
	assignments, err := pluginsrepo.New(tx).ListPluginAssignments(ctx, result.Plugin.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, "*", assignments[0].PrincipalUrn)
}

// TestEnsureDefaultPlugin_NonDefaultProjectSeedsNoAudience pins that only the
// org's default project gets the org-wide audience: a Default plugin created in
// a second (non-default) project starts with no assignments, so enabling a
// server there doesn't auto-broadcast to the whole org.
func TestEnsureDefaultPlugin_NonDefaultProjectSeedsNoAudience(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// A second project created after the default one — its uuidv7 id sorts after
	// the org's original project, so it is not the default (oldest) project.
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "second-project",
		Slug:           "second-project",
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)

	result, err := plugins.EnsureDefaultPlugin(ctx, tx, authCtx.ActiveOrganizationID, other.ID)
	require.NoError(t, err)
	require.True(t, result.Created)

	assignments, err := pluginsrepo.New(tx).ListPluginAssignments(ctx, result.Plugin.ID)
	require.NoError(t, err)
	require.Empty(t, assignments,
		"a non-default project's Default plugin starts with no audience")
}

func TestEnsureDefaultPlugin_ReturnsExistingWhenPresent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Separate transactions, each committed — mirrors the real calling
	// convention (one transaction per request) and exercises the savepoint
	// retry path across genuinely distinct transactions, not just one.
	tx1 := testenv.BeginTx(t, ctx, ti.conn)
	first, err := plugins.EnsureDefaultPlugin(ctx, tx1, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.NoError(t, tx1.Commit(ctx))

	tx2 := testenv.BeginTx(t, ctx, ti.conn)
	second, err := plugins.EnsureDefaultPlugin(ctx, tx2, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Equal(t, first.Plugin.ID, second.Plugin.ID)
}

func TestEnsureDefaultPlugin_PromotesExistingDefaultSlugPlugin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	// A plugin already occupies the "Default"/"default" name+slug (e.g.
	// created manually before this feature shipped) but isn't marked
	// is_default. CreateDefaultPlugin can never succeed for this project, so
	// EnsureDefaultPlugin must heal by promoting the existing plugin instead
	// of failing forever.
	existing, err := queries.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Default",
		Slug:           "default",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	result, err := plugins.EnsureDefaultPlugin(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	require.NoError(t, err)
	require.False(t, result.Created)
	require.Equal(t, existing.ID, result.Plugin.ID)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, result.Plugin.IsDefault)
}

func TestAttachToDefaultPlugin_DisplayNameCollision_Uniquifies(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	first := createTestMcpServer(t, ctx, ti.conn, "Attach Test Server", mcpservers.VisibilityPublic)
	tx1 := testenv.BeginTx(t, ctx, ti.conn)
	result, err := plugins.AttachToDefaultPlugin(ctx, tx1, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{},
		McpServerID:    uuid.NullUUID{UUID: first.id, Valid: true},
		DisplayName:    "Attach Test Server",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, tx1.Commit(ctx))

	// A different mcp_server deriving the same display name must neither be
	// silently dropped nor block the attach (the caller's triggering action —
	// e.g. enabling the server — would fail with it): the name is uniquified
	// with the backend-id suffix instead.
	second := createTestMcpServer(t, ctx, ti.conn, "Attach Test Server 2", mcpservers.VisibilityPublic)
	tx2 := testenv.BeginTx(t, ctx, ti.conn)
	attached, err := plugins.AttachToDefaultPlugin(ctx, tx2, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{},
		McpServerID:    uuid.NullUUID{UUID: second.id, Valid: true},
		DisplayName:    "Attach Test Server",
	})
	require.NoError(t, err)
	require.NotNil(t, attached)
	require.NoError(t, tx2.Commit(ctx))

	idStr := second.id.String()
	require.Equal(t, fmt.Sprintf("Attach Test Server (%s)", idStr[len(idStr)-4:]), attached.Server.DisplayName)

	servers, err := queries.ListPluginServers(ctx, result.PluginID)
	require.NoError(t, err)
	require.Len(t, servers, 2, "both same-named servers must be attached")
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
		ProjectID:                *authCtx.ProjectID,
		InstallationID:           12345,
		RepoOwner:                "test-org",
		RepoName:                 "test-project-plugins",
		MarketplaceToken:         pgtype.Text{String: "test-token", Valid: true},
		PublishedMcpFingerprints: []byte(`{}`),
		PublishedHooksVersion:    pgtype.Text{String: "test-hooks-version", Valid: true},
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
