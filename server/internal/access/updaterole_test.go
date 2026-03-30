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

func TestService_UpdateRole(t *testing.T) {
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
	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "member")
	ti.roles.AddMember("org_workos_test", "membership_2", "user_2", "member")
	ti.roles.AddMember("org_workos_test", "membership_3", "user_3", "custom-builder")

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeBuildRead, "project-old")

	name := "Platform Builder"
	description := "Updated description"
	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          "role_custom",
		Name:        &name,
		Description: &description,
		Grants: []*gen.RoleGrant{
			{Scope: string(access.ScopeBuildWrite), Resources: []string{"project-1", "project-2"}},
			{Scope: string(access.ScopeMCPConnect), Resources: nil},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	require.NoError(t, err)
	require.Equal(t, "role_custom", role.ID)
	require.Equal(t, name, role.Name)
	require.Equal(t, description, role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 3, role.MemberCount)
	require.Equal(t, thirdpartyworkos.MockRoleTimestamp(), role.CreatedAt)
	require.Equal(t, thirdpartyworkos.MockRoleTimestamp(), role.UpdatedAt)
	require.Len(t, role.Grants, 2)

	roles, err := ti.roles.ListRoles(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, name, roles[0].Name)
	require.Equal(t, description, roles[0].Description)
	require.Equal(t, "custom-builder", roles[0].Slug)

	grants, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{PrincipalUrn: new(urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder").String())})
	require.NoError(t, err)
	require.Len(t, grants.Grants, 3)

	members, err := ti.roles.ListMembers(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, members, 3)
	require.Equal(t, "custom-builder", members[0].RoleSlug)
	require.Equal(t, "custom-builder", members[1].RoleSlug)
	require.Equal(t, "custom-builder", members[2].RoleSlug)
}

func TestService_UpdateRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateRole_WorkOSUpdateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{
		ID:          "role_custom",
		Name:        "Custom Builder",
		Slug:        "custom-builder",
		Description: "Old description",
	})
	ti.roles.SetUpdateRoleError(errors.New("workos unavailable"))

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update role in workos")
}
