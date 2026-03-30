package access_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_ListMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_admin", Name: "Admin", Slug: "admin", Description: ""})
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_builder", Name: "Builder", Slug: "custom-builder", Description: ""})
	ti.roles.AddUser(thirdpartyworkos.User{ID: "user_1", FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com"})
	ti.roles.AddUser(thirdpartyworkos.User{ID: "user_2", FirstName: "Grace", LastName: "", Email: "grace@example.com"})
	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "admin")
	ti.roles.AddMember("org_workos_test", "membership_2", "user_2", "custom-builder")

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Members, 2)

	byID := map[string]*gen.AccessMember{}
	for _, member := range result.Members {
		byID[member.ID] = member
	}

	require.Equal(t, "Ada Lovelace", byID["user_1"].Name)
	require.Equal(t, "ada@example.com", byID["user_1"].Email)
	require.Equal(t, "role_admin", byID["user_1"].RoleID)
	require.Nil(t, byID["user_1"].PhotoURL)
	require.Equal(t, time.Time{}.UTC().Format(time.RFC3339), byID["user_1"].JoinedAt)

	require.Equal(t, "Grace", byID["user_2"].Name)
	require.Equal(t, "role_builder", byID["user_2"].RoleID)
}

func TestService_ListMembers_WorkOSUsersFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_admin", Name: "Admin", Slug: "admin", Description: ""})
	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "admin")
	ti.roles.SetListOrgUsersError(errors.New("workos unavailable"))

	_, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list org users from workos")
}
