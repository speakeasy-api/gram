package plugins_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/stretchr/testify/require"
)

func TestProcessRoleDistributionSetup_RefusesPackageDirectoryCollision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, existing, role string }{
		{name: "codex package", existing: "sales", role: "Sales Codex"},
		{name: "claude package", existing: "sales-codex", role: "Sales"},
		{name: "platform package", existing: "other", role: "Speakeasy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			_, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: tc.existing, Slug: &tc.existing})
			require.NoError(t, err)
			before, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
			require.NoError(t, err)
			role := createTestRolePrincipal(t, ctx, ti, tc.role)
			err = processDirectRoleSetup(ctx, ti, role, plugins.PublicationRequests{})
			require.ErrorIs(t, err, plugins.ErrPluginSlugUnavailable)
			listed, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
			require.NoError(t, err)
			require.Len(t, listed.Plugins, len(before.Plugins), "a refused setup must not leave a partial role plugin")
		})
	}
}

// Turning auto sync off is refused, with nothing written, while one of the
// plugin's package directories is also another plugin's or the observability
// plugin's: no automatic publish could keep it as it was last published. The
// refusal is a client error that names the directory, not the unsynced
// changes refusal. Once the slugs no longer collide, auto sync turns off.
func TestPluginsService_RefusesCollidingPauseBeforeFirstPublish(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := newAutoSyncFixtureFor(t, ctx, ti, nil)
	observability, _ := orgObservabilitySlugs(t, ctx, ti)
	seeded := make(map[string]*gen.Plugin)
	for _, slug := range []string{"legacy", "legacy-codex", observability} {
		plugin, err := pluginsrepo.New(ti.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
			Name: slug, Slug: slug, Description: pgtype.Text{}, AutoSyncEnabled: pgtype.Bool{},
		})
		require.NoError(t, err)
		seeded[slug] = &gen.Plugin{ID: plugin.ID.String()}
	}
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)

	for slug, shared := range map[string]string{
		"legacy":       "legacy-codex/ with plugin legacy-codex",
		"legacy-codex": "legacy-codex/ with plugin legacy",
		observability:  observability + "/ with the observability plugin",
	} {
		_, err := f.setAutoSync(ctx, seeded[slug], false)
		requirePackagesSharedRefusal(t, err, shared)
		got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: seeded[slug].ID})
		require.NoError(t, err)
		require.True(t, got.AutoSyncEnabled, "a refused change leaves auto sync on for %s", slug)
	}
	afterAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit, afterAudit, "a refused change writes nothing")

	moved, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: seeded["legacy-codex"].ID, Name: "legacy-codex", Slug: "legacy-tools"})
	require.NoError(t, err)
	for _, plugin := range []*gen.Plugin{seeded["legacy"], moved} {
		paused, err := f.setAutoSync(ctx, plugin, false)
		require.NoError(t, err)
		require.False(t, paused.AutoSyncEnabled)
	}
}

func TestPluginsService_OrgRenameDefersCollidingHooksUntilSlugIsFree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		paused bool
	}{{name: "auto sync on"}, {name: "auto sync off", paused: true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockGitHubPublisher{}
			features := &feature.InMemory{}
			ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
			f := newAutoSyncFixtureFor(t, ctx, ti, mock)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Existing tools", Slug: new("renamed-team-observability")})
			require.NoError(t, err)
			f.addServer(t, ctx, plugin, "rename-collision-tools", "Tools", 0)
			_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
			require.NoError(t, err)
			if tc.paused {
				_, err = f.setAutoSync(ctx, plugin, false)
				require.NoError(t, err)
			}
			oldSlug, _ := orgObservabilitySlugs(t, ctx, ti)
			oldHooks := filesWithPrefix(mock.lastPushedFiles, oldSlug+"/")
			require.NotEmpty(t, oldHooks)
			features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version":9999}`))
			renameOrganization(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "Renamed Team")
			f.automaticPublish(t, ctx)
			require.Equal(t, oldHooks, filesWithPrefix(mock.lastPushedFiles, oldSlug+"/"))
			require.Contains(t, string(mock.lastPushedFiles[plugin.Slug+"/.claude-plugin/plugin.json"]), `"name": "renamed-team-observability"`)
			status, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
			require.NoError(t, err)
			require.NotNil(t, status.UpToDate)
			require.Equal(t, !tc.paused, *status.UpToDate, "only the paused plugin itself has changes waiting after the rename")
			mock.pushFilesCalled = false
			require.True(t, f.automaticPublish(t, ctx).Skipped)
			require.False(t, mock.pushFilesCalled)

			if tc.paused {
				_, err = f.setAutoSync(ctx, plugin, true)
				require.NoError(t, err)
			}
			_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: plugin.Name, Slug: "renamed-tools"})
			require.NoError(t, err)
			f.automaticPublish(t, ctx)
			require.Empty(t, filesWithPrefix(mock.lastPushedFiles, oldSlug+"/"))
			require.Contains(t, string(mock.lastPushedFiles["renamed-team-observability/.claude-plugin/plugin.json"]), "Observability")
		})
	}
}
