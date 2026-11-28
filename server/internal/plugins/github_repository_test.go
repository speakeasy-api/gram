package plugins_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

func TestPublishRefusesUnverifiedRepositoryNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		errs []error
	}{
		{"other identity conflict", []error{plugins.ErrGitHubRepoConflict}},
		{"both names taken", []error{ghclient.ErrRepositoryNameTaken, ghclient.ErrRepositoryNameTaken}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockGitHubPublisher{createRepoErrors: tc.errs}
			ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
			f := newAutoSyncFixtureFor(t, ctx, ti, mock)
			p, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Bound plugin"})
			require.NoError(t, err)
			f.addServer(t, ctx, p, "binding-server", "Test Server", 0)
			_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{GithubUsernames: []string{"test-user"}})
			require.ErrorIs(t, err, plugins.ErrGitHubRepoConflict)
			require.Len(t, mock.createRepoNames, len(tc.errs))
			require.Empty(t, mock.scopedRepoName)
			require.False(t, mock.pushFilesCalled)
			require.False(t, mock.addCollaboratorCalled)
			require.False(t, mock.getRepoFilesCalled)
			_, err = repo.New(ti.conn).GetPluginRepositoryBinding(ctx, f.projectID)
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}

func TestPublishNameFallbackRetriedFromBinding(t *testing.T) {
	t.Parallel()
	pushErr := errors.New("simulated push failure")
	mock := &mockGitHubPublisher{
		createRepoErrors: []error{ghclient.ErrRepositoryNameTaken},
		pushFilesErr:     pushErr,
	}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	f := newAutoSyncFixtureFor(t, ctx, ti, mock)
	p, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Bound plugin"})
	require.NoError(t, err)
	f.addServer(t, ctx, p, "binding-server", "Test Server", 0)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectSlug = conv.PtrEmpty(strings.Repeat("slug", 24))
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	preferred := strings.ToLower(authCtx.OrganizationSlug + "-" + *authCtx.ProjectSlug + "-plugins")
	require.Greater(t, len(preferred), 100)
	suffix := "-" + f.projectID.String()
	fallback := preferred[:100-len(suffix)] + suffix
	payload := &gen.PublishPluginsPayload{GithubUsernames: []string{"test-user"}}

	_, err = ti.service.PublishPlugins(ctx, payload)
	require.ErrorIs(t, err, pushErr)
	require.Equal(t, []string{preferred[:100], fallback}, mock.createRepoNames)
	require.Equal(t, fallback, mock.pushedRepoName)
	require.False(t, mock.addCollaboratorCalled)
	queries := repo.New(ti.conn)
	_, err = queries.GetGitHubConnection(ctx, f.projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	binding, err := queries.GetPluginRepositoryBinding(ctx, f.projectID)
	require.NoError(t, err)
	require.Equal(t, fallback, binding.RepoName)
	require.Positive(t, binding.RepositoryID)

	// Neither a failed first publish nor a subsequent slug change can move the
	// project off the repo it already created and bound.
	authCtx.ProjectSlug = conv.PtrEmpty("renamed-project")
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	mock.pushFilesErr = nil
	result, err := ti.service.PublishPlugins(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/test-org/"+fallback, result.RepoURL)
	require.Equal(t, fallback, mock.pushedRepoName)
	require.Equal(t, fallback, mock.scopedRepoName)
	require.Equal(t, fallback, mock.collaboratorRepoName)
	require.Equal(t, binding.RepositoryID, mock.scopedRepoID)
	conn, err := queries.GetGitHubConnection(ctx, f.projectID)
	require.NoError(t, err)
	require.Equal(t, fallback, conn.RepoName)
	_, err = ti.service.PublishPlugins(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, fallback, mock.pushedRepoName)
	require.Equal(t, []string{preferred[:100], fallback}, mock.createRepoNames)
}

func TestPublishRegeneratesOnVerifiedReplacement(t *testing.T) {
	t.Parallel()
	ctx, f := newAutoSyncFixture(t, "replacement")
	queries := repo.New(f.ti.conn)
	binding, err := queries.GetPluginRepositoryBinding(ctx, f.projectID)
	require.NoError(t, err)
	const replacement = "verified-replacement"
	replacementID := mockGitHubRepositoryIDs.Add(1)
	_, err = f.ti.conn.Exec(ctx, `UPDATE plugin_repository_bindings SET repo_name = $2, repository_id = $3 WHERE project_id = $1`, f.projectID, replacement, replacementID)
	require.NoError(t, err)
	_, err = f.ti.conn.Exec(ctx, `UPDATE plugin_github_connections SET repo_name = $2, published_fingerprint = NULL, published_mcp_fingerprints = NULL, published_hooks_version = NULL, published_hooks_config = NULL WHERE project_id = $1`, f.projectID, replacement)
	require.NoError(t, err)
	f.mock.createRepoNames = nil
	f.mock.getRepoFilesCalled = false
	f.mock.getRepoFilesErr = ghclient.ErrRepoNotFound
	f.mock.lastPushedFiles = nil

	result := f.automaticPublish(t, ctx)
	require.False(t, result.Skipped)
	require.Equal(t, "https://github.com/test-org/"+replacement, result.RepoURL)
	require.Empty(t, f.mock.createRepoNames)
	require.False(t, f.mock.getRepoFilesCalled)
	require.Equal(t, replacement, f.mock.pushedRepoName)
	require.Equal(t, replacementID, f.mock.scopedRepoID)
	require.NotEqual(t, binding.RepositoryID, f.mock.scopedRepoID)
	conn, err := queries.GetGitHubConnection(ctx, f.projectID)
	require.NoError(t, err)
	require.Equal(t, replacement, conn.RepoName)
	require.NotEmpty(t, conn.PublishedMcpFingerprints)
	require.NotEmpty(t, conn.PublishedHooksVersion.String)
}

func TestPublishRefusesUnverifiedLegacyConnection(t *testing.T) {
	t.Parallel()
	ctx, f := newAutoSyncFixture(t, "binding")
	// A pre-migration project has publication metadata but no trusted repository ID.
	_, err := f.ti.conn.Exec(ctx, "DELETE FROM plugin_repository_bindings WHERE project_id = $1", f.projectID)
	require.NoError(t, err)
	f.mock.createRepoCalled = false
	f.mock.pushFilesCalled = false
	f.mock.getRepoFilesCalled = false
	_, err = f.ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{GithubUsernames: []string{"test-user"}})
	require.ErrorIs(t, err, plugins.ErrGitHubRepoConflict)
	require.False(t, f.mock.createRepoCalled)
	require.False(t, f.mock.getRepoFilesCalled)
	require.False(t, f.mock.pushFilesCalled)
	require.False(t, f.mock.addCollaboratorCalled)
}
