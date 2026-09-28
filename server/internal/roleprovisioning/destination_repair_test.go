package roleprovisioning_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/stretchr/testify/require"
)

func TestConfigureExplicitDestinationResolvesPendingRoles(t *testing.T) {
	for _, initial := range []string{"no-projects", "explicit-pending"} {
		t.Run(initial, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			if initial == "no-projects" {
				f.exec(`DELETE FROM projects WHERE id=$1`, f.project)
			}
			pending := uuid.Nil
			input := roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, Enabled: true}
			if initial == "explicit-pending" {
				input.ProjectID = &pending
			}
			v, err := f.service.Configure(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, "choose_project", f.reconcile(f.role).Pending)
			x := f.addProject(f.org, "chosen")
			override := f.addProject(f.org, "override")
			overriddenRole := f.addRole(f.org, "Override")
			excludedRole := f.addRole(f.org, "Excluded")
			clearedRole := f.addRole(f.org, "Explicit pending")
			selectedRole := f.addRole(f.org, "Selected pending")
			v = f.configure(v, false,
				roleprovisioning.Selection{RoleURN: overriddenRole, Enabled: true, ProjectID: &override},
				roleprovisioning.Selection{RoleURN: excludedRole, Enabled: false})
			v = f.configure(v, true)
			require.Equal(t, "choose_project", f.reconcile(f.role).Pending, "re-enable alone must not choose a destination")
			input = roleprovisioning.ConfigureInput{Actor: f.actor, OrganizationID: f.org, ExpectedVersion: v, Enabled: true, ProjectID: &x, Roles: []roleprovisioning.Selection{
				{RoleURN: clearedRole, Enabled: true, ProjectID: &pending},
				{RoleURN: selectedRole, Enabled: true},
			}}
			next, err := f.service.Configure(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, v+1, next)
			settings, err := f.service.Settings(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, uuid.NullUUID{UUID: x, Valid: true}, settings.ProjectID)
			byRole := map[string]roleprovisioning.RoleSettings{}
			for _, setting := range settings.Roles {
				byRole[setting.RoleURN] = setting
			}
			require.Equal(t, uuid.NullUUID{UUID: x, Valid: true}, byRole[f.role].ProjectID, "explicit organization destination must fill pending roles")
			require.Equal(t, uuid.NullUUID{UUID: override, Valid: true}, byRole[overriddenRole].ProjectID)
			require.False(t, byRole[excludedRole].Enabled)
			require.Equal(t, uuid.NullUUID{UUID: x, Valid: true}, byRole[selectedRole].ProjectID, "omitted per-role destination inherits the explicit organization choice")
			require.False(t, byRole[clearedRole].ProjectID.Valid, "explicit role pending in the same save wins")
			require.Empty(t, f.reconcile(f.role).Pending)
			require.Equal(t, "choose_project", f.reconcile(clearedRole).Pending)
			_, err = f.service.Configure(t.Context(), input)
			require.ErrorIs(t, err, roleprovisioning.ErrConflict)
			unchanged, err := f.service.Settings(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, settings, unchanged)
		})
	}
}
