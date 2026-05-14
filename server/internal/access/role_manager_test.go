package access

import (
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestRoleManager_ListRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	customID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))

	roles, err := ti.service.roleMgr.ListRoles(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, roles.Roles, 2)

	bySlug := map[string]string{}
	for _, role := range roles.Roles {
		if role.Name == "Admin" {
			bySlug["admin"] = role.ID
		}
		if role.Name == "Custom Builder" {
			bySlug["custom-builder"] = role.ID
		}
	}
	require.Equal(t, adminID, bySlug["admin"])
	require.Equal(t, customID, bySlug["custom-builder"])
}

func TestRoleManager_GetRoleByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	customID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))

	role, err := ti.service.roleMgr.GetRoleByID(ctx, authCtx.ActiveOrganizationID, customID)
	require.NoError(t, err)
	require.Equal(t, customID, role.ID)
	require.Equal(t, "Custom Builder", role.Name)

	_, err = ti.service.roleMgr.GetRoleByID(ctx, authCtx.ActiveOrganizationID, "not-a-uuid")
	require.Error(t, err)
}

func TestRoleManager_MembersAndCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "u2@example.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_3", "user_3", "custom-builder"))

	manager := ti.service.roleMgr
	members, err := manager.ListMembers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, members.Members, 3)

	slugs, err := manager.MemberRoleSlugs(ctx, authCtx.ActiveOrganizationID, "user_2")
	require.NoError(t, err)
	require.Equal(t, []string{"custom-builder"}, slugs)

	counts, err := manager.memberCounts(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, 1, counts["admin"])
	require.Equal(t, 1, counts["custom-builder"])
}
