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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_UpdateMemberRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom-builder"}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		RoleSlugs:      []string{"custom-builder"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, mockidp.MockOrgID).Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_builder"}})
	require.NoError(t, err)
	require.Equal(t, "local_user_1", member.ID)
	require.Equal(t, "Ada Lovelace", member.Name)
	require.Equal(t, "ada@example.com", member.Email)
	require.Equal(t, []string{"role_builder"}, member.RoleIds)
	require.Nil(t, member.PhotoURL)
	require.Equal(t, mockMembershipTimestamp, member.JoinedAt)
}

func TestService_UpdateMemberRole_MultipleRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"admin", "custom-builder"}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "admin",
		RoleSlugs:      []string{"admin", "custom-builder"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, mockidp.MockOrgID).Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_admin", "role_builder"}})
	require.NoError(t, err)
	require.Equal(t, []string{"role_admin", "role_builder"}, member.RoleIds)
}

func TestService_UpdateMemberRole_EmptyRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one role is required")
}

func TestService_UpdateMemberRole_RoleNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{}, nil).Once()

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_missing"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateMemberRole_MemberNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "user_missing", RoleIds: []string{"role_builder"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member has not joined this organization")
}

func TestService_UpdateMemberRole_WorkOSMembershipNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_builder"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member not found")
}

func TestService_UpdateMemberRole_WorkOSFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom-builder"}).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_builder"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update member roles in workos")
}

func TestService_UpdateMemberRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom-builder"}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		RoleSlugs:      []string{"custom-builder"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, mockidp.MockOrgID).Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_builder"}})
	require.NoError(t, err)
	require.NotNil(t, member)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessMemberRoleUpdate), record.Action)
	require.Equal(t, "access_member", record.SubjectType)
	require.Equal(t, "Ada Lovelace", record.SubjectDisplay)
	require.Equal(t, "ada@example.com", record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.NotNil(t, beforeSnapshot["RoleIds"])
	require.NotNil(t, afterSnapshot["RoleIds"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_UpdateMemberRole_LastAdminProtection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleIds: []string{"role_builder"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot remove the last admin role")
}
