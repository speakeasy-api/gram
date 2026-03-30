package access_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_DeleteRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{
		ID:          "role_custom",
		Name:        "Custom Builder",
		Slug:        "custom-builder",
		Description: "Old description",
	})
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeBuildRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeMCPConnect, access.WildcardResource)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.NoError(t, err)

	roles, err := ti.roles.ListRoles(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Empty(t, roles)

	grants, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{PrincipalUrn: new(urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder").String())})
	require.NoError(t, err)
	require.Empty(t, grants.Grants)
}

func TestService_DeleteRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_DeleteRole_SystemRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.AddSystemRole("org_workos_test", "role_admin", "Admin", "admin")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_admin"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system roles cannot be deleted")
}

func TestService_DeleteRole_WorkOSDeleteFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{
		ID:          "role_custom",
		Name:        "Custom Builder",
		Slug:        "custom-builder",
		Description: "Old description",
	})
	ti.roles.SetDeleteRoleError(errors.New("workos unavailable"))

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete role in workos")
}
