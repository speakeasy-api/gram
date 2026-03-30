package access_test

import (
	"errors"
	"testing"
	"time"

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

	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_admin", Name: "Admin", Slug: "admin", Description: ""})
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_builder", Name: "Builder", Slug: "custom-builder", Description: ""})
	ti.roles.AddUser(thirdpartyworkos.User{ID: "user_1", FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com"})
	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "admin")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_builder"})
	require.NoError(t, err)
	require.Equal(t, "local_user_1", member.ID)
	require.Equal(t, "Ada Lovelace", member.Name)
	require.Equal(t, "ada@example.com", member.Email)
	require.Equal(t, "role_builder", member.RoleID)
	require.Nil(t, member.PhotoURL)
	require.Equal(t, time.Time{}.UTC().Format(time.RFC3339), member.JoinedAt)

	members, err := ti.roles.ListMembers(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, "custom-builder", members[0].RoleSlug)
}

func TestService_UpdateMemberRole_RoleNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "admin")

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
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_builder", Name: "Builder", Slug: "custom-builder", Description: ""})
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
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_builder", Name: "Builder", Slug: "custom-builder", Description: ""})
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
	ti.roles.AddRole("org_workos_test", thirdpartyworkos.Role{ID: "role_builder", Name: "Builder", Slug: "custom-builder", Description: ""})
	ti.roles.AddUser(thirdpartyworkos.User{ID: "user_1", FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com"})
	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "admin")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	ti.roles.SetUpdateMemberRoleError(errors.New("workos unavailable"))

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "role_builder"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update member role in workos")
}
