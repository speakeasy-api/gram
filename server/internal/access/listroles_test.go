package access_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
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

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
		mockMember("org_workos_test", "membership_2", "user_2", "custom-builder"),
		mockMember("org_workos_test", "membership_3", "user_3", "custom-builder"),
	}, nil).Once()

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "admin"), access.ScopeOrgAdmin, access.WildcardResource)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeBuildRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeBuildRead, "project-2")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), access.ScopeMCPConnect, access.WildcardResource)

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Roles, 2)

	rolesBySlug := make(map[string]*gen.Role, len(result.Roles))
	for _, role := range result.Roles {
		rolesBySlug[role.Slug] = role
	}

	adminRole := rolesBySlug["admin"]
	require.NotNil(t, adminRole)
	require.Equal(t, "Admin", adminRole.Name)
	require.True(t, adminRole.IsSystem)
	require.Equal(t, 1, adminRole.MemberCount)
	require.Equal(t, mockRoleTimestamp, adminRole.CreatedAt)
	require.Equal(t, mockRoleTimestamp, adminRole.UpdatedAt)
	require.Len(t, adminRole.Grants, 1)
	require.Equal(t, string(access.ScopeOrgAdmin), adminRole.Grants[0].Scope)
	require.Nil(t, adminRole.Grants[0].Resources)

	customRole := rolesBySlug["custom-builder"]
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
	require.ElementsMatch(t, []string{"project-1", "project-2"}, grantsByScope[string(access.ScopeBuildRead)].Resources)
	require.Nil(t, grantsByScope[string(access.ScopeMCPConnect)].Resources)
}
