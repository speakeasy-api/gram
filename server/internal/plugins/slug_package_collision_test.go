package plugins_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func requireSlugUnavailable(t *testing.T, err error, message string) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, message, oopsErr.Error())
}

// Plugin foo publishes its Codex package to foo-codex/, the directory plugin
// foo-codex would publish its Claude package to, so the second of the two is
// refused whichever comes first, on create and on a slug change alike.
func TestPluginsService_RefusesSlugsWhosePackagesShareADirectory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Foo", Slug: new("foo")})
	require.NoError(t, err)
	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Foo Codex", Slug: new("foo-codex")})
	requireSlugUnavailable(t, err, "invalid slug: foo-codex would publish into foo-codex/, which is already used by plugin foo; choose a different slug")

	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Bar Codex", Slug: new("bar-codex")})
	require.NoError(t, err)
	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Bar", Slug: new("bar")})
	requireSlugUnavailable(t, err, "invalid slug: bar would publish into bar-codex/, which is already used by plugin bar-codex; choose a different slug")

	// A slug derived from the name is checked the same way.
	_, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Foo Codex"})
	requireSlugUnavailable(t, err, "invalid slug: foo-codex would publish into foo-codex/, which is already used by plugin foo; choose a different slug")

	other, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Other", Slug: new("other")})
	require.NoError(t, err)
	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: other.ID, Name: other.Name, Slug: "foo-codex"})
	requireSlugUnavailable(t, err, "invalid slug: foo-codex would publish into foo-codex/, which is already used by plugin foo; choose a different slug")

	// Renaming a plugin without moving its slug needs no check.
	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: other.ID, Name: "Other Renamed", Slug: other.Slug})
	require.NoError(t, err)

	listed, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	slugs := make([]string, 0, len(listed.Plugins))
	for _, p := range listed.Plugins {
		slugs = append(slugs, p.Slug)
	}
	require.NotContains(t, slugs, "foo-codex")
	require.NotContains(t, slugs, "bar")
}

// Slugs naming the directories the marketplace publishes its own packages to
// are refused: the per-platform package roots, the Platform MCP plugin, and
// the organization's observability plugin.
func TestPluginsService_RefusesSlugsOfTheMarketplacesOwnPackages(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	observability, _ := orgObservabilitySlugs(t, ctx, ti)

	for slug, message := range map[string]string{
		"cursor-plugins":            "invalid slug: cursor-plugins would publish into cursor-plugins/, which is already used by the marketplace's Cursor packages; choose a different slug",
		"speakeasy":                 "invalid slug: speakeasy would publish into speakeasy/, which is already used by the Speakeasy Platform MCP plugin; choose a different slug",
		observability:               "invalid slug: " + observability + " would publish into " + observability + "/, which is already used by the observability plugin; choose a different slug",
		observability + "-opencode": "invalid slug: " + observability + "-opencode would publish into " + observability + "-opencode/, which is already used by the observability plugin; choose a different slug",
	} {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: slug, Slug: new(slug)})
			requireSlugUnavailable(t, err, message)
		})
	}
}

// A project created before the check may already hold both foo and
// foo-codex. Its publishes keep working, including an automatic one while
// foo's auto sync is off.
func TestPluginsService_ExistingSharedDirectoryKeepsPublishing(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	f := newAutoSyncFixtureFor(t, ctx, ti, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seeded := make(map[string]*gen.Plugin)
	for _, slug := range []string{"foo", "foo-codex"} {
		row, err := pluginsrepo.New(ti.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: f.projectID, Name: slug, Slug: slug,
			Description: pgtype.Text{String: "", Valid: false}, AutoSyncEnabled: pgtype.Bool{Bool: false, Valid: false},
		})
		require.NoError(t, err)
		plugin, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: row.ID.String()})
		require.NoError(t, err)
		seeded[slug] = plugin
		f.addServer(t, ctx, plugin, "shared-dir-"+slug, slug+" One", 0)
	}

	_, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	_, err = f.setAutoSync(ctx, seeded["foo"], false)
	require.NoError(t, err)
	f.addServer(t, ctx, seeded["foo"], "shared-dir-foo-two", "foo Two", 1)
	f.addServer(t, ctx, seeded["foo-codex"], "shared-dir-foo-codex-two", "foo-codex Two", 1)

	// Neither plugin can be held, since both write into foo-codex/, so the
	// paused one publishes its live state.
	result := f.automaticPublish(t, ctx)
	require.False(t, result.Skipped)
	pushed := f.mock.lastPushedFiles
	require.Contains(t, string(pushed["foo/.mcp.json"]), "foo Two")
	require.Contains(t, string(pushed["foo-codex/.claude-plugin/plugin.json"]), `"name": "foo-codex"`)
}
