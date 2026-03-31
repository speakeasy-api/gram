package access_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_UpdateMemberRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
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
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, "org_workos_test").Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_builder"})
	require.NoError(t, err)
	require.Equal(t, "local_user_1", member.ID)
	require.Equal(t, "Ada Lovelace", member.Name)
	require.Equal(t, "ada@example.com", member.Email)
	require.Equal(t, "role_builder", member.RoleID)
	require.Nil(t, member.PhotoURL)
	require.Equal(t, mockMembershipTimestamp, member.JoinedAt)
}

func TestService_UpdateMemberRole_RoleNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{}, nil).Once()

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateMemberRole_MemberNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "user_missing", RoleID: "role_builder"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member is not connected locally")
}

func TestService_UpdateMemberRole_WorkOSMembershipNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{}, nil).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_builder"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member not found")
}

func TestService_UpdateMemberRole_WorkOSFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
	}, nil).Once()
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Once()
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_builder"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update member role in workos")
}
