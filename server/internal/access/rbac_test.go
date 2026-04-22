package access

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListRoles_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_ListRoles_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgRead, Resource: testAccessAuthContext(t, ctx).ActiveOrganizationID})

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
	}, nil).Once()

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Roles, 1)
}

func TestService_GetRole_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_GetRole_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgRead, Resource: authCtx.ActiveOrganizationID})

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "custom-builder"),
	}, nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")

	role, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	require.NoError(t, err)
	require.Equal(t, "role_custom", role.ID)
}

func TestService_ListScopes_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_ListScopes_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgRead, Resource: testAccessAuthContext(t, ctx).ActiveOrganizationID})

	result, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Scopes, 7)
}

func TestService_ListMembers_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_ListMembers_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgRead, Resource: authCtx.ActiveOrganizationID})

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, "org_workos_test").Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
	}, nil).Once()

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Members, 1)
}

func TestService_CreateRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{Name: "Denied", Description: "Denied"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_CreateRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgAdmin, Resource: testAccessAuthContext(t, ctx).ActiveOrganizationID})

	ti.roles.On("CreateRole", mock.Anything, "org_workos_test", thirdpartyworkos.CreateRoleOpts{
		Name:        "Allowed",
		Slug:        "org-allowed",
		Description: "Allowed",
	}).Return(&thirdpartyworkos.Role{
		ID:          "role_allowed",
		Name:        "Allowed",
		Slug:        "org-allowed",
		Description: "Allowed",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{Name: "Allowed", Description: "Allowed"})
	require.NoError(t, err)
	require.Equal(t, "role_allowed", role.ID)
}

func TestService_UpdateRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_custom"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_UpdateRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})
	name := "Updated"

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{}, nil).Once()
	ti.roles.On("UpdateRole", mock.Anything, "org_workos_test", "custom-builder", thirdpartyworkos.UpdateRoleOpts{Name: &name}).Return(&thirdpartyworkos.Role{
		ID:          "role_custom",
		Name:        name,
		Slug:        "custom-builder",
		Description: "Old",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{}, nil).Once()

	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: "role_custom", Name: &name})
	require.NoError(t, err)
	require.Equal(t, name, role.Name)
}

func TestService_DeleteRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_DeleteRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, "org_workos_test", "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.NoError(t, err)
}

func TestService_UpdateMemberRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "user_1", RoleID: "role_builder"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_UpdateMemberRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, Grant{Scope: ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: "org_workos_test",
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp.Format(time.RFC3339),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, "org_workos_test").Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_builder"})
	require.NoError(t, err)
	require.Equal(t, "role_builder", member.RoleID)
}

func withRBACGrants(t *testing.T, ctx context.Context, grants ...Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return GrantsToContext(ctx, &Grants{rows: grants})
}

func testAccessAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}
