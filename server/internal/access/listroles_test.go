package access

import (
	mockidp "github.com/speakeasy-api/gram/server/internal/testenv/testidp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_3", "user_3", "custom-builder"),
		// user_workos_only has never logged into Gram — should not be counted
		mockMember(mockidp.MockOrgID, "membership_workos_only", "user_workos_only", "custom-builder"),
	}, nil).Once()

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_3", "user3@test.com", "User 3", "user_3", "membership_3")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "admin"), authz.ScopeOrgAdmin, authz.WildcardResource)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-2")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, authz.WildcardResource)

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Roles, 2)

	rolesByID := make(map[string]*gen.Role, len(result.Roles))
	for _, role := range result.Roles {
		rolesByID[role.ID] = role
	}

	adminRole := rolesByID["role_admin"]
	require.NotNil(t, adminRole)
	require.Equal(t, "Admin", adminRole.Name)
	require.True(t, adminRole.IsSystem)
	require.Equal(t, 1, adminRole.MemberCount)
	require.Equal(t, mockRoleTimestamp, adminRole.CreatedAt)
	require.Equal(t, mockRoleTimestamp, adminRole.UpdatedAt)
	require.Len(t, adminRole.Grants, 1)
	require.Equal(t, string(authz.ScopeOrgAdmin), adminRole.Grants[0].Scope)
	require.Nil(t, adminRole.Grants[0].Selectors)

	customRole := rolesByID["role_custom"]
	require.NotNil(t, customRole)
	require.Equal(t, "Custom Builder", customRole.Name)
	require.False(t, customRole.IsSystem)
	require.Equal(t, 2, customRole.MemberCount)
	require.Equal(t, "Can build selected resources", customRole.Description)
	require.Len(t, customRole.Grants, 2)

	grantsByScope := make(map[string]*gen.RoleGrant, len(customRole.Grants))
	for _, grant := range customRole.Grants {
		grantsByScope[grant.Scope] = grant
	}
	sels := grantsByScope[string(authz.ScopeProjectRead)].Selectors
	ids := make([]string, len(sels))
	for i, s := range sels {
		ids[i] = s.ResourceID
	}
	require.ElementsMatch(t, []string{"project-1", "project-2"}, ids)
	require.Nil(t, grantsByScope[string(authz.ScopeMCPConnect)].Selectors)
}

func TestService_ListRoles_ExcludesDisconnectedUsersFromMemberCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// user_1 is connected to the org.
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	// user_2 exists locally with a workos_id but is NOT connected to this org
	// (no organization_user_relationships row). Should not inflate member count.
	seedDisconnectedUser(t, ctx, ti.conn, "local_user_2", "user2@test.com", "User 2", "user_2")

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		// user_2 appears in WorkOS members but is disconnected locally.
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "admin"),
	}, nil).Once()

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Roles, 1)
	// Only user_1 should be counted — user_2 has a local account but no org connection.
	require.Equal(t, 1, result.Roles[0].MemberCount)
}
