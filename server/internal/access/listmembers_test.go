package access

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	trequire "github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_ListMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
		mockRole("role_builder", "Builder", "custom-builder", ""),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
		mockMember("org_workos_test", "membership_2", "user_2", "custom-builder"),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, "org_workos_test").Return(map[string]thirdpartyworkos.User{
		"user_1": mockUser("user_1", "Ada", "Lovelace", "ada@example.com"),
		"user_2": mockUser("user_2", "Grace", "", "grace@example.com"),
	}, nil).Once()

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	trequire.NoError(t, err)
	trequire.Len(t, result.Members, 2)

	byID := map[string]*gen.AccessMember{}
	for _, member := range result.Members {
		byID[member.ID] = member
	}

	trequire.Equal(t, "Ada Lovelace", byID["user_1"].Name)
	trequire.Equal(t, "ada@example.com", byID["user_1"].Email)
	trequire.Equal(t, "role_admin", byID["user_1"].RoleID)
	trequire.Nil(t, byID["user_1"].PhotoURL)
	trequire.Equal(t, "2024-11-15T15:04:05Z", byID["user_1"].JoinedAt)

	trequire.Equal(t, "Grace", byID["user_2"].Name)
	trequire.Equal(t, "role_builder", byID["user_2"].RoleID)
}

func TestService_ListMembers_WorkOSUsersFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()
	ti.roles.On("ListMembers", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Member{
		mockMember("org_workos_test", "membership_1", "user_1", "admin"),
	}, nil).Once()
	ti.roles.On("ListOrgUsers", mock.Anything, "org_workos_test").Return(map[string]thirdpartyworkos.User(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "list org users from workos")
}
