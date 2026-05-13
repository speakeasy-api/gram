package access

import (
	"context"
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
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
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ListRoles_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Roles, 1)
}

func TestService_GetRole_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: "role_custom"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_GetRole_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build selected resources"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	role, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: roleID})
	require.NoError(t, err)
	require.Equal(t, roleID, role.ID)
}

func TestService_ListScopes_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ListScopes_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, testAccessAuthContext(t, ctx).ActiveOrganizationID)})

	result, err := ti.service.ListScopes(ctx, &gen.ListScopesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Scopes, 9)
}

func TestService_ListMembers_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ListMembers_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Members, 1)
}

func TestService_CreateRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{Name: "Denied", Description: "Denied"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_CreateRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, testAccessAuthContext(t, ctx).ActiveOrganizationID)})

	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
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
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_UpdateRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	name := "Updated"

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old"))
	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, "custom-builder", thirdpartyworkos.UpdateRoleOpts{Name: &name}).Return(&thirdpartyworkos.Role{
		ID:          "role_custom",
		Name:        name,
		Slug:        "custom-builder",
		Description: "Old",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()

	role, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: roleID, Name: &name})
	require.NoError(t, err)
	require.Equal(t, name, role.Name)
}

func TestService_DeleteRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_DeleteRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)
}

func TestService_UpdateMemberRole_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "user_1", RoleID: "role_builder"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_UpdateMemberRole_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: builderID})
	require.NoError(t, err)
	require.Equal(t, builderID, member.RoleID)
}

func withRBACGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return authz.GrantsToContext(ctx, grants)
}

func testAccessAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}
