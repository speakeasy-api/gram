package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_ListMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	// Seed local users so that the WorkOS-to-Gram ID resolution succeeds.
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "grace@example.com", "Grace", "user_2", "membership_2")

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, mockidp.MockOrgID).Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
		"user_2": mockUser("user_2", "Grace", "", "grace@example.com"),
	}, nil).Once()

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Members, 2)

	byID := map[string]*gen.AccessMember{}
	for _, member := range result.Members {
		byID[member.ID] = member
	}

	// IDs should be Gram user IDs, not WorkOS user IDs.
	require.Equal(t, "Ada Lovelace", byID["local_user_1"].Name)
	require.Equal(t, "ada@example.com", byID["local_user_1"].Email)
	require.Equal(t, "role_admin", byID["local_user_1"].RoleID)
	require.Equal(t, "2024-11-15T15:04:05Z", byID["local_user_1"].JoinedAt)

	require.Equal(t, "Grace", byID["local_user_2"].Name)
	require.Equal(t, "role_builder", byID["local_user_2"].RoleID)
}

func TestService_ListMembers_ExcludesDisconnectedUsers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	// user_1 is connected to the org (has organization_user_relationships row).
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	// user_2 exists in the users table with a workos_id but is NOT connected
	// to this org — no row in organization_user_relationships.
	seedDisconnectedUser(t, ctx, ti.conn, "local_user_2", "grace@example.com", "Grace", "user_2")

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
		mockMember(mockidp.MockOrgID, "membership_2", "user_2", "admin"),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, mockidp.MockOrgID).Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
		"user_2": mockUser("user_2", "Grace", "", "grace@example.com"),
	}, nil).Once()

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Members, 1, "disconnected user should be excluded")
	require.Equal(t, "local_user_1", result.Members[0].ID)
	require.Equal(t, "Ada Lovelace", result.Members[0].Name)
}

func TestService_ListMembers_WorkOSUsersFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Member{
		mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, mockidp.MockOrgID).Return(map[string]thirdpartyworkos.User(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list org users from workos")
}
