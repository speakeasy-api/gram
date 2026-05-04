package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
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

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeMCPConnect, authz.WildcardResource)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.NoError(t, err)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Empty(t, grants)
}

func TestService_DeleteRole_ReassignsMembersToDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_other", "user_3", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", authz.SystemRoleMember).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       authz.SystemRoleMember,
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_2", authz.SystemRoleMember).Return(&thirdpartyworkos.Member{
		ID:             "membership_2",
		UserID:         "user_2",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       authz.SystemRoleMember,
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.NoError(t, err)
}

func TestService_DeleteRole_ReassignFailureHaltsDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", authz.SystemRoleMember).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reassign member to default role")

	// Grants must remain since reassignment failed before grant cleanup ran.
	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Len(t, grants, 1)
}

func TestService_DeleteRole_PartialReassignFailureStopsLoop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	// Iteration order over the slice is deterministic, so seed an explicit
	// success-then-failure pair to exercise the partial-failure cache flush.
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom-builder"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", authz.SystemRoleMember).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       authz.SystemRoleMember,
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_2", authz.SystemRoleMember).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reassign member to default role")

	// The mock's AssertExpectations (registered in newMockRoleProvider) verifies
	// that DeleteRole was never called and the loop stopped at the first failure.
	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	require.Len(t, grants, 1)
}

func TestService_DeleteRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{}, nil).Once()

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_DeleteRole_SystemRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_admin"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "system roles cannot be deleted")
}

func TestService_DeleteRole_WorkOSDeleteFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(errors.New("workos unavailable")).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete role in workos")

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

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Audit Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, mockidp.MockOrgID, "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), authz.ScopeProjectRead, "project-1")

	err = ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
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
