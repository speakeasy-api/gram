package roleprovisioning_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/stretchr/testify/require"
)

func TestSettingsDefaultOffAndNullable(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	settings, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.False(t, settings.ProjectID.Valid)
	require.Zero(t, settings.Version)
	require.Empty(t, settings.Roles)
	require.True(t, f.reconcile(f.role).Skipped)
	f.exec(`INSERT INTO organization_role_provisioning_settings (organization_id,enabled,project_id,version) VALUES ($1,NULL,NULL,NULL)`, f.org)
	settings, err = f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.False(t, settings.ProjectID.Valid)
	require.Zero(t, settings.Version)
	require.Empty(t, settings.Roles)
	require.True(t, f.reconcile(f.role).Skipped)
	require.Zero(t, f.count(`SELECT count(*) FROM plugins`))
	require.Zero(t, f.count(`SELECT count(*) FROM publish_outbox`))
}

func TestConfigureSelectionsExclusionsAndStaleVersion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	excluded := f.addRole(f.org, "Excluded")
	v := f.configure(0, true, roleprovisioning.Selection{RoleURN: excluded, Enabled: false})
	require.Equal(t, 1, f.count(`SELECT count(*) FROM publish_outbox WHERE topic='gram.plugins.v1.RoleProvisioningRequested'`))
	require.Zero(t, f.count(`SELECT count(*) FROM plugins`), "saving intent must not provision")
	_, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: 0, Enabled: false, Roles: []roleprovisioning.Selection{{RoleURN: excluded, Enabled: true}}})
	require.ErrorIs(t, err, roleprovisioning.ErrConflict)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM publish_outbox WHERE topic='gram.plugins.v1.RoleProvisioningRequested'`), "stale save must not emit a hint")
	settings, err := f.service.Settings(t.Context(), f.org)
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, v, settings.Version)
	require.True(t, settings.ProjectID.Valid)
	require.Equal(t, f.project, settings.ProjectID.UUID)
	f.configure(v, true)
	require.True(t, f.reconcile(excluded).Skipped)
	f.assertEmpty(f.reconcile(f.role).PluginID)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_provisioning_settings WHERE role_urn=$1 AND NOT enabled`, excluded))
}

func TestConfigureUnseenRolesUsePreviousOrganizationState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		previous bool
		next     bool
	}{
		{name: "disabled to enabled", previous: false, next: true},
		{name: "enabled to disabled", previous: true, next: false},
		{name: "enabled remains enabled", previous: true, next: true},
		{name: "disabled remains disabled", previous: false, next: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			excluded := f.addRole(f.org, "Excluded")
			v := f.configure(0, tc.previous, roleprovisioning.Selection{RoleURN: excluded, Enabled: false})
			unseen := f.addRole(f.org, "Unseen")
			explicit := f.addRole(f.org, "Explicit")
			v = f.configure(v, tc.next, roleprovisioning.Selection{RoleURN: explicit, Enabled: !tc.previous})
			saved, err := f.service.Settings(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, tc.next, saved.Enabled)
			require.Equal(t, v, saved.Version)
			selections := make(map[string]bool, len(saved.Roles))
			for _, role := range saved.Roles {
				selections[role.RoleURN] = role.Enabled
			}
			require.Equal(t, map[string]bool{
				f.role:   true,
				excluded: false,
				unseen:   tc.previous,
				explicit: !tc.previous,
			}, selections)
		})
	}
}

func TestInitialOrganizationAndGlobalRolesAreEmptyOriginOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	global := f.addRole("", "Global engineers")
	defaultID := uuid.New()
	f.exec(`INSERT INTO plugins (id,organization_id,project_id,name,slug,is_default) VALUES ($1,$2,$3,'Default','default',true)`, defaultID, f.org, f.project)
	f.exec(`INSERT INTO plugin_assignments (plugin_id,organization_id,principal_urn) VALUES ($1,$2,'*')`, defaultID, f.org)
	f.configure(0, true)
	for _, role := range []string{f.role, global} {
		result := f.reconcile(role)
		require.Empty(t, result.Pending)
		require.False(t, result.Skipped)
		require.Equal(t, plugins.ProjectPublicationEmissionDisabled, result.Publication)
		require.NotEqual(t, defaultID, result.PluginID)
		f.assertEmpty(result.PluginID)
		require.Equal(t, []string{role}, f.audiences(result.PluginID))
		require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins WHERE id=$1 AND NOT is_default`, result.PluginID))
	}
	require.Zero(t, f.count(`SELECT count(*) FROM organization_role_assignments`))
	require.Equal(t, []string{"*"}, f.audiences(defaultID))
}

func TestRetryAndConcurrentWorkersReuseOnePlugin(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	const workers = 8
	results := make([]roleprovisioning.Result, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { <-start; results[i], errs[i] = f.service.Reconcile(t.Context(), f.org, f.role, f.actor) })
	}
	close(start)
	wg.Wait()
	id := results[0].PluginID
	require.NotEqual(t, uuid.Nil, id)
	for i := range workers {
		require.NoError(t, errs[i])
		require.Equal(t, id, results[i].PluginID)
		require.Empty(t, results[i].Pending)
	}
	before := f.count(`SELECT count(*) FROM publish_outbox`)
	require.Equal(t, id, f.reconcile(f.role).PluginID)
	require.Equal(t, before, f.count(`SELECT count(*) FROM publish_outbox`), "no-op retry must not emit events")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE is_current`))
	require.Equal(t, []string{f.role}, f.audiences(id))
}

func TestRemapABAReusesPluginAndPreservesContentsAndOtherAudiences(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := f.addProject(f.org, "second")
	other := f.addRole(f.org, "Other audience")
	v := f.configure(0, true)
	aID := f.reconcile(f.role).PluginID
	toolset := uuid.New()
	f.exec(`INSERT INTO toolsets (id,organization_id,project_id,name,slug) VALUES ($1,$2,$3,'Retained','retained')`, toolset, f.org, f.project)
	f.exec(`INSERT INTO plugin_servers (plugin_id,toolset_id,display_name) VALUES ($1,$2,'Retained')`, aID, toolset)
	f.exec(`INSERT INTO plugin_assignments (plugin_id,organization_id,principal_urn) VALUES ($1,$2,$3)`, aID, f.org, other)
	v = f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &b})
	moved := f.reconcile(f.role)
	require.Empty(t, moved.Pending)
	bID := moved.PluginID
	require.NotEqual(t, aID, bID)
	f.assertEmpty(bID)
	require.Equal(t, []string{other}, f.audiences(aID))
	require.Equal(t, []string{f.role}, f.audiences(bID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1 AND toolset_id=$2 AND NOT deleted`, aID, toolset))
	f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &f.project})
	returned := f.reconcile(f.role)
	require.Empty(t, returned.Pending)
	require.Equal(t, aID, returned.PluginID)
	require.ElementsMatch(t, []string{f.role, other}, f.audiences(aID))
	require.Empty(t, f.audiences(bID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1 AND toolset_id=$2 AND NOT deleted`, aID, toolset))
	require.Equal(t, 2, f.count(`SELECT count(*) FROM role_plugin_associations`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND is_current`, aID))
}

func TestDisabledDoesNotRepairOrRename(t *testing.T) {
	t.Parallel()
	for _, orgDisabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "organization", false: "role"}[orgDisabled], func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			v := f.configure(0, true)
			id := f.reconcile(f.role).PluginID
			f.exec(`DELETE FROM plugin_assignments WHERE plugin_id=$1`, id)
			f.exec(`UPDATE organization_roles SET workos_name='Renamed' WHERE organization_id=$1`, f.org)
			if orgDisabled {
				f.configure(v, false)
			} else {
				f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: false})
			}
			before := f.count(`SELECT count(*) FROM publish_outbox`)
			require.True(t, f.reconcile(f.role).Skipped)
			require.Empty(t, f.audiences(id))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins WHERE id=$1 AND name='Engineers'`, id))
			require.Equal(t, before, f.count(`SELECT count(*) FROM publish_outbox`))
		})
	}
}

func TestMissingDestinationStaysPending(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"no-project", "explicit-null", "deleted-project"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			if mode == "no-project" {
				f.exec(`DELETE FROM projects WHERE id=$1`, f.project)
			}
			if mode == "explicit-null" {
				_, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, Enabled: true, ProjectID: new(uuid.Nil)})
				require.NoError(t, err)
			} else {
				f.configure(0, true)
			}
			if mode == "deleted-project" {
				f.exec(`UPDATE projects SET deleted_at=now() WHERE id=$1`, f.project)
			}
			result := f.reconcile(f.role)
			require.Equal(t, "choose_project", result.Pending)
			require.Equal(t, uuid.Nil, result.PluginID)
			require.Zero(t, f.count(`SELECT count(*) FROM plugins`))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM role_provisioning_settings WHERE role_urn=$1 AND last_error_code='choose_project' AND last_attempt_at IS NOT NULL`, f.role))
		})
	}
}

func TestAutomaticNamesRespectMismatchAndManualMarker(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"automatic", "mismatch", "manual-marker"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.configure(0, true)
			id := f.reconcile(f.role).PluginID
			want := "Renamed"
			switch mode {
			case "mismatch":
				f.exec(`UPDATE plugins SET name='Manual' WHERE id=$1`, id)
				want = "Manual"
			case "manual-marker":
				f.exec(`UPDATE role_plugin_associations SET last_automatic_name=NULL WHERE plugin_id=$1`, id)
				want = "Engineers"
			}
			f.exec(`UPDATE organization_roles SET workos_name='Renamed' WHERE organization_id=$1`, f.org)
			require.Equal(t, id, f.reconcile(f.role).PluginID)
			require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins WHERE id=$1 AND name=$2`, id, want))
			if mode != "automatic" {
				require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND last_automatic_name IS NULL`, id))
			}
		})
	}
}

func TestDeletedPluginGetsNewEmptyIncarnation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	old := f.reconcile(f.role).PluginID
	toolset := uuid.New()
	f.exec(`INSERT INTO toolsets (id,organization_id,project_id,name,slug) VALUES ($1,$2,$3,'Old content','old-content')`, toolset, f.org, f.project)
	f.exec(`INSERT INTO plugin_servers (plugin_id,toolset_id,display_name) VALUES ($1,$2,'Old content')`, old, toolset)
	f.exec(`UPDATE plugins SET deleted_at=now() WHERE id=$1`, old)
	replacement := f.reconcile(f.role)
	require.Empty(t, replacement.Pending)
	require.NotEqual(t, old, replacement.PluginID)
	f.assertEmpty(replacement.PluginID)
	require.Equal(t, []string{f.role}, f.audiences(replacement.PluginID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND retired_at IS NOT NULL AND NOT is_current`, old))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1`, old))
}

func TestCrossOrganizationConfigurationRejectedAtomically(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	foreignOrg := "org_other_fixture"
	f.exec(`INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Other fixture','other-fixture')`, foreignOrg)
	foreignProject := f.addProject(foreignOrg, "foreign")
	foreignRole := f.addRole(foreignOrg, "Foreign role")
	inputs := []roleprovisioning.ConfigureInput{
		{Actor: f.actor, OrganizationID: f.org, Enabled: true, ProjectID: &foreignProject},
		{Actor: f.actor, OrganizationID: f.org, Enabled: true, Roles: []roleprovisioning.Selection{{RoleURN: foreignRole, Enabled: true}}},
		{Actor: f.actor, OrganizationID: f.org, Enabled: true, Roles: []roleprovisioning.Selection{{RoleURN: f.role, Enabled: true, ProjectID: &foreignProject}}},
	}
	for _, input := range inputs {
		_, err := f.service.Configure(t.Context(), input)
		require.ErrorIs(t, err, roleprovisioning.ErrInvalid)
		require.Zero(t, f.count(`SELECT count(*) FROM organization_role_provisioning_settings`))
		require.Zero(t, f.count(`SELECT count(*) FROM role_provisioning_settings`))
		require.Zero(t, f.count(`SELECT count(*) FROM publish_outbox`))
	}
	f.configure(0, true)
	require.True(t, f.reconcile(foreignRole).Skipped)
	require.Zero(t, f.count(`SELECT count(*) FROM plugins`))
	f.exec(`UPDATE role_provisioning_settings SET project_id=$1 WHERE role_urn=$2`, foreignProject, f.role)
	require.Equal(t, "choose_project", f.reconcile(f.role).Pending)
	require.Zero(t, f.count(`SELECT count(*) FROM plugins`))
}
