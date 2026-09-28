package roleprovisioning_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/stretchr/testify/require"
)

func TestProjectSelectionTieAndStickyDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := f.addProject(f.org, "second")
	f.exec(`UPDATE projects SET created_at='2026-01-01T00:00:00Z' WHERE organization_id=$1`, f.org)
	expected := f.project
	if b.String() < expected.String() {
		expected = b
	}
	v := f.configure(0, true)
	saved, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, expected, saved.ProjectID.UUID)
	other := b
	if other == expected {
		other = f.project
	}
	f.exec(`INSERT INTO plugins (organization_id,project_id,name,slug) VALUES ($1,$2,'Manual','manual')`, f.org, other)
	v = f.configure(v, false)
	f.configure(v, true)
	saved, err = f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, expected, saved.ProjectID.UUID)
	require.Equal(t, expected, saved.Roles[0].ProjectID.UUID)
}

func TestSlugCollisionDoesNotAdoptManualPlugin(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	manual := uuid.New()
	slug := "engineers-o-" + strings.TrimPrefix(f.role, "role:organization:")
	f.exec(`INSERT INTO plugins (id,organization_id,project_id,name,slug) VALUES ($1,$2,$3,'Engineers',$4)`, manual, f.org, f.project, slug)
	f.configure(0, true)
	created := f.reconcile(f.role)
	require.NotEqual(t, manual, created.PluginID)
	require.NotEqual(t, uuid.Nil, created.PluginID)
	require.Empty(t, f.audiences(manual))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins WHERE id=$1 AND slug=$2`, created.PluginID, slug+"-1"))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1`, created.PluginID))
}

func TestAudienceReplacementDurablyHintsOriginRepair(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := f.addRole(f.org, "Other")
	f.configure(0, true)
	pluginID := f.reconcile(f.role).PluginID
	before := f.count(`SELECT count(*) FROM publish_outbox WHERE topic LIKE '%RoleProvisioningRequested%'`)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for lock and rollback assertions
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	require.NoError(t, admission.LockProject(t.Context(), tx, f.project))
	locked, err := assignments.Lock(t.Context(), tx, f.org, f.project, pluginID)
	require.NoError(t, err)
	_, err = assignments.Replace(t.Context(), tx, audit.NewLogger(), locked, assignments.Input{OrganizationID: f.org, ProjectID: f.project, PluginID: pluginID, PrincipalURNs: []string{other}, Actor: f.actor.Principal}, assignments.Dependencies{Guard: assignments.LegacyGuard})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	require.Equal(t, before+1, f.count(`SELECT count(*) FROM publish_outbox WHERE topic LIKE '%RoleProvisioningRequested%'`))
	require.Equal(t, []string{other}, f.audiences(pluginID))
	repaired := f.reconcile(f.role)
	require.Equal(t, pluginID, repaired.PluginID)
	require.ElementsMatch(t, []string{f.role, other}, f.audiences(pluginID))
	// Maintenance must not self-enqueue another maintenance hint.
	require.Equal(t, before+1, f.count(`SELECT count(*) FROM publish_outbox WHERE topic LIKE '%RoleProvisioningRequested%'`))
}

func TestInactiveRoleAndDeletedProjectFailClosed(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"role", "project"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.configure(0, true)
			if target == "role" {
				f.exec(`UPDATE organization_roles SET deleted_at=now() WHERE organization_id=$1`, f.org)
				require.True(t, f.reconcile(f.role).Skipped)
			} else {
				f.exec(`UPDATE projects SET deleted_at=now() WHERE id=$1`, f.project)
				require.Equal(t, "choose_project", f.reconcile(f.role).Pending)
			}
			require.Zero(t, f.count(`SELECT count(*) FROM plugins`))
		})
	}
}

func TestConcurrentInitialConfigurationConflicts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, Enabled: true})
			results <- err
		}()
	}
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, roleprovisioning.ErrConflict)
	} else {
		require.ErrorIs(t, first, roleprovisioning.ErrConflict)
		require.NoError(t, second)
	}
	require.Equal(t, 1, f.count(`SELECT count(*) FROM organization_role_provisioning_settings`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_provisioning_settings`))
}
