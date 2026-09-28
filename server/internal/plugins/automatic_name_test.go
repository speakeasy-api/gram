package plugins_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestUpdatePluginAutomaticNameOwnership(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, displayName, slug string
		description             *string
		cleared                 bool
	}{
		{name: "same name", displayName: "Automatic", slug: "automatic"},
		{name: "slug only", displayName: "Automatic", slug: "custom-slug"},
		{name: "description only", displayName: "Automatic", slug: "automatic", description: conv.PtrEmpty("Custom description")},
		{name: "display name", displayName: "Custom", slug: "automatic", cleared: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Automatic"})
			require.NoError(t, err)
			associationID := uuid.New()
			_, err = ti.conn.Exec(ctx, `INSERT INTO role_plugin_associations (id, project_id, plugin_id, last_automatic_name) VALUES ($1, $2, $3, $4)`, associationID, *ac.ProjectID, uuid.MustParse(plugin.ID), plugin.Name) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
			require.NoError(t, err)
			// Wrong tenant or project must not clear even an exact plugin ID.
			q := pluginsrepo.New(ti.conn)
			for _, scope := range []pluginsrepo.ClearRolePluginAutomaticNameParams{
				{PluginID: uuid.MustParse(plugin.ID), OrganizationID: "other-org", ProjectID: *ac.ProjectID},
				{PluginID: uuid.MustParse(plugin.ID), OrganizationID: ac.ActiveOrganizationID, ProjectID: uuid.New()},
			} {
				require.NoError(t, q.ClearRolePluginAutomaticName(ctx, scope))
			}
			var original string
			require.NoError(t, ti.conn.QueryRow(ctx, `SELECT last_automatic_name FROM role_plugin_associations WHERE id = $1`, associationID).Scan(&original)) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
			require.Equal(t, plugin.Name, original)
			_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: tc.displayName, Slug: tc.slug, Description: tc.description})
			require.NoError(t, err)
			var automaticName pgtype.Text
			err = ti.conn.QueryRow(ctx, `SELECT last_automatic_name FROM role_plugin_associations WHERE id = $1`, associationID).Scan(&automaticName) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
			require.NoError(t, err)
			require.Equal(t, !tc.cleared, automaticName.Valid)
			if tc.cleared {
				// Returning to the old display name must not re-enable automation.
				_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Automatic", Slug: tc.slug})
				require.NoError(t, err)
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT last_automatic_name FROM role_plugin_associations WHERE id = $1`, associationID).Scan(&automaticName)) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
				require.False(t, automaticName.Valid)
			}
			if !tc.cleared {
				require.Equal(t, "Automatic", automaticName.String)
			}
		})
	}
}

// A concurrent automatic rename can make a submitted name a no-op. The human
// writer must compare against the locked before-image, not its pre-wait value.
func TestUpdatePluginAutomaticNameLockedBeforeImage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Automatic"})
	require.NoError(t, err)
	associationID := uuid.New()
	_, err = ti.conn.Exec(ctx, `INSERT INTO role_plugin_associations (id, project_id, plugin_id, last_automatic_name) VALUES ($1,$2,$3,$4)`, associationID, *ac.ProjectID, uuid.MustParse(plugin.ID), plugin.Name) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	require.NoError(t, err)
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for lock and rollback assertions
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	var blocker int
	require.NoError(t, tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM plugins WHERE id=$1 FOR UPDATE`, uuid.MustParse(plugin.ID)).Scan(&blocker)) //nolint:glint // notestingrawsql: observe database lock ordering in the isolated fixture
	done := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: plugin.ID, Name: "Renamed", Slug: plugin.Slug})
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := ti.conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting) //nolint:glint // notestingrawsql: observe database lock ordering in the isolated fixture
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	_, err = tx.Exec(ctx, `UPDATE plugins SET name='Renamed' WHERE id=$1`, uuid.MustParse(plugin.ID)) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE role_plugin_associations SET last_automatic_name='Renamed' WHERE id=$1`, associationID) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("name update did not finish")
	}
	var marker string
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT last_automatic_name FROM role_plugin_associations WHERE id=$1`, associationID).Scan(&marker)) //nolint:glint // notestingrawsql: isolated retention, manual-edit, and failure-injection fixtures
	require.Equal(t, "Renamed", marker)
}
