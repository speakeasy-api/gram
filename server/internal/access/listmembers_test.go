package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestService_ListMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)

	// Seed local users so that the WorkOS-to-Gram ID resolution succeeds.
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "grace@example.com", "Grace", "user_2", "membership_2")

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember("", "membership_2", "user_2", "custom-builder"))

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
	require.Equal(t, adminID, byID["local_user_1"].RoleID)
	require.NotEmpty(t, byID["local_user_1"].JoinedAt)

	require.Equal(t, "Grace", byID["local_user_2"].Name)
	require.Equal(t, builderID, byID["local_user_2"].RoleID)
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

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember("", "membership_2", "user_2", "admin"))

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Members, 1, "disconnected user should be excluded")
	require.Equal(t, "local_user_1", result.Members[0].ID)
	require.Equal(t, "Ada Lovelace", result.Members[0].Name)
}

func TestService_ListMembers_UsesDatabaseOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember("", "membership_1", "user_1", "admin"))

	result, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Members)
}
