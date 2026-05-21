package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
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

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, authz.WildcardResource)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Empty(t, grants)

	role, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosSlug:     "custom-builder",
	})
	require.NoError(t, err)
	require.True(t, role.Deleted)
	require.False(t, role.WorkosDeleted)
	require.False(t, role.WorkosDeletedAt.Valid)
}

func TestService_DeleteRole_ReassignsMembersToDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	memberRoleID := seedGlobalRole(t, ctx, ti.conn, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "user1@test.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "user2@test.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_other", "user_3", "admin"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{authz.SystemRoleMember}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       authz.SystemRoleMember,
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_2", []string{authz.SystemRoleMember}).Return(&thirdpartyworkos.Member{
		ID:             "membership_2",
		UserID:         "user_2",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       authz.SystemRoleMember,
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)

	members, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	membersByID := map[string]*gen.AccessMember{}
	for _, member := range members.Members {
		membersByID[member.ID] = member
	}
	require.Equal(t, []string{memberRoleID}, membersByID["local_user_1"].RoleIds)
	require.Equal(t, []string{memberRoleID}, membersByID["local_user_2"].RoleIds)

	roles, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	for _, role := range roles.Roles {
		if role.ID == memberRoleID {
			require.Equal(t, 2, role.MemberCount)
			return
		}
	}
	require.Fail(t, "member role not found")
}

func TestService_DeleteRole_ReassignFailureDoesNotHaltDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{authz.SystemRoleMember}).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Times(3)
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Empty(t, grants)
}

func TestService_DeleteRole_PartialReassignFailureContinuesDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{authz.SystemRoleMember}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       authz.SystemRoleMember,
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_2", []string{authz.SystemRoleMember}).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Times(3)
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Empty(t, grants)
}

func TestService_DeleteRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "00000000-0000-0000-0000-000000000001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_DeleteRole_SystemRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system roles cannot be deleted")
}

func TestService_DeleteRole_WorkOSDeleteFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"))
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(errors.New("workos unavailable")).Times(3)
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Empty(t, grants)
}

func TestService_DeleteRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleDelete)
	require.NoError(t, err)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Audit Builder", "custom-builder", "Old description"))
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err = ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleDelete)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessRoleDelete), record.Action)
	require.Equal(t, "access_role", record.SubjectType)
	require.Equal(t, "Audit Builder", record.SubjectDisplay)
	require.Equal(t, "custom-builder", record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
