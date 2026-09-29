package access

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRoleProvisioningRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, ac.ActiveOrganizationID)})
	_, err := ti.service.GetRoleProvisioning(ctx, &gen.GetRoleProvisioningPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ConfigureRoleProvisioning(ctx, &gen.ConfigureRoleProvisioningPayload{Enabled: true})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.GetRoleProvisioning(t.Context(), &gen.GetRoleProvisioningPayload{})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestRoleProvisioningAPIIntentConflictAndIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ac := testAccessAuthContext(t, ctx)
	seedRole(t, ctx, ti.conn, ac.ActiveOrganizationID, mockRole("role_provisioning_fixture", "Provisioning", "provisioning", ""))
	before, err := ti.service.GetRoleProvisioning(ctx, &gen.GetRoleProvisioningPayload{})
	require.NoError(t, err)
	require.False(t, before.Enabled)
	require.Zero(t, before.Version)
	pending := uuid.Nil.String()
	saved, err := ti.service.ConfigureRoleProvisioning(ctx, &gen.ConfigureRoleProvisioningPayload{Enabled: true, ProjectID: &pending})
	require.NoError(t, err)
	require.True(t, saved.Enabled)
	require.EqualValues(t, 1, saved.Version)
	require.NotEmpty(t, saved.Roles)
	for _, r := range saved.Roles {
		require.Nil(t, r.PluginID)
		require.Equal(t, "not_provisioned", r.PublicationStatus)
		require.Equal(t, "choose_project", *r.PendingReason)
	}
	_, err = ti.service.ConfigureRoleProvisioning(ctx, &gen.ConfigureRoleProvisioningPayload{Enabled: false})
	requireOopsCode(t, err, oops.CodeConflict)
	foreignOrganizationID := "org_" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, foreignOrganizationID)
	foreignProjectID := seedProject(t, ctx, ti.conn, foreignOrganizationID).String()
	foreignRoleID := seedRole(t, ctx, ti.conn, foreignOrganizationID, mockRole("role_foreign_provisioning_fixture", "Foreign provisioning", "foreign-provisioning", ""))
	_, err = ti.service.ConfigureRoleProvisioning(ctx, &gen.ConfigureRoleProvisioningPayload{ExpectedVersion: 1, Enabled: true, ProjectID: &foreignProjectID})
	requireOopsCode(t, err, oops.CodeBadRequest)
	latest, err := ti.service.GetRoleProvisioning(ctx, &gen.GetRoleProvisioningPayload{})
	require.NoError(t, err)
	require.Equal(t, saved, latest)
	_, err = ti.service.ConfigureRoleProvisioning(ctx, &gen.ConfigureRoleProvisioningPayload{ExpectedVersion: 1, Enabled: true, Roles: []*gen.RoleProvisioningSelection{{RoleUrn: "role:organization:" + foreignRoleID, Enabled: true}}})
	requireOopsCode(t, err, oops.CodeBadRequest)
	latest, err = ti.service.GetRoleProvisioning(ctx, &gen.GetRoleProvisioningPayload{})
	require.NoError(t, err)
	require.Equal(t, saved, latest)
	disabled, err := ti.service.ConfigureRoleProvisioning(ctx, &gen.ConfigureRoleProvisioningPayload{ExpectedVersion: 1, Enabled: false})
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.EqualValues(t, 2, disabled.Version)
	for _, r := range disabled.Roles {
		require.True(t, r.Enabled)
		require.Nil(t, r.PendingReason)
	}
}
