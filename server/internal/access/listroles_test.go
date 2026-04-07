package access

import (
	"testing"

	"github.com/stretchr/testify/mock"
	trequire "github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
		mockMember("org_workos_test", "membership_2", "user_2", "custom-builder"),
		mockMember("org_workos_test", "membership_3", "user_3", "custom-builder"),
	}, nil).Once()

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "admin"), ScopeOrgAdmin, WildcardResource)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-2")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeMCPConnect, WildcardResource)

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	trequire.NoError(t, err)
	trequire.Len(t, result.Roles, 2)

	rolesByID := make(map[string]*gen.Role, len(result.Roles))
	for _, role := range result.Roles {
		rolesByID[role.ID] = role
	}

	adminRole := rolesByID["role_admin"]
	trequire.NotNil(t, adminRole)
	trequire.Equal(t, "Admin", adminRole.Name)
	trequire.True(t, adminRole.IsSystem)
	trequire.Equal(t, 1, adminRole.MemberCount)
	trequire.Equal(t, mockRoleTimestamp, adminRole.CreatedAt)
	trequire.Equal(t, mockRoleTimestamp, adminRole.UpdatedAt)
	trequire.Len(t, adminRole.Grants, 1)
	trequire.Equal(t, string(ScopeOrgAdmin), adminRole.Grants[0].Scope)
	trequire.Nil(t, adminRole.Grants[0].Resources)

	customRole := rolesByID["role_custom"]
	trequire.NotNil(t, customRole)
	trequire.Equal(t, "Custom Builder", customRole.Name)
	trequire.False(t, customRole.IsSystem)
	trequire.Equal(t, 2, customRole.MemberCount)
	trequire.Equal(t, "Can build selected resources", customRole.Description)
	trequire.Len(t, customRole.Grants, 2)

	grantsByScope := make(map[string]*gen.RoleGrant, len(customRole.Grants))
	for _, grant := range customRole.Grants {
		grantsByScope[grant.Scope] = grant
	}
	trequire.ElementsMatch(t, []string{"project-1", "project-2"}, grantsByScope[string(ScopeBuildRead)].Resources)
	trequire.Nil(t, grantsByScope[string(ScopeMCPConnect)].Resources)
}
