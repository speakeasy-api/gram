package access

import (
	"testing"
	"time"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestRoleManager_ListRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	customID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))

	roles, err := ti.service.roleMgr.ListRoles(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, roles.Roles, 2)

	bySlug := map[string]string{}
	for _, role := range roles.Roles {
		if role.Name == "Admin" {
			bySlug["admin"] = role.ID
		}
		if role.Name == "Custom Builder" {
			bySlug["custom-builder"] = role.ID
		}
	}
	require.Equal(t, adminID, bySlug["admin"])
	require.Equal(t, customID, bySlug["custom-builder"])
}

func TestRoleManager_GetRoleByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	customID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))

	role, err := ti.service.roleMgr.GetRoleByID(ctx, authCtx.ActiveOrganizationID, customID)
	require.NoError(t, err)
	require.Equal(t, customID, role.ID)
	require.Equal(t, "Custom Builder", role.Name)

	_, err = ti.service.roleMgr.GetRoleByID(ctx, authCtx.ActiveOrganizationID, "not-a-uuid")
	require.Error(t, err)
}

func TestRoleManager_MembersAndCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "u2@example.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_3", "user_3", "custom-builder"))

	manager := ti.service.roleMgr
	members, err := manager.ListMembers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, members.Members, 3)

	rolePrincipals, err := manager.MemberRolePrincipals(ctx, authCtx.ActiveOrganizationID, "user_2")
	require.NoError(t, err)
	slugs := make([]string, 0, len(rolePrincipals))
	for _, role := range rolePrincipals {
		slugs = append(slugs, role.RoleSlug)
	}
	require.Equal(t, []string{"custom-builder"}, slugs)

	roles, err := manager.ListRoles(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	counts := make(map[string]int, len(roles.Roles))
	for _, role := range roles.Roles {
		counts[role.Name] = role.MemberCount
	}
	require.Equal(t, 1, counts["Admin"])
	require.Equal(t, 1, counts["Custom Builder"])
}

func TestRoleManager_AssignMembersToRoleAcceptsConnectedMemberWithoutAssignment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")

	assigned, _, err := ti.service.roleMgr.assignMembersToRoleTx(ctx, ti.conn, authCtx.ActiveOrganizationID, "custom-builder", []string{"local_user_1"})
	require.NoError(t, err)
	require.Equal(t, 1, assigned)
}

func TestRoleManager_LocalRoleWritePreservesWorkOSLastEventID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	now := time.Now().UTC()
	_, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "custom-builder",
		WorkosName:        "Custom Builder",
		WorkosDescription: conv.ToPGTextEmpty("Before"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGText("event_01SEED"),
	})
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "custom-builder",
		WorkosName:        "Custom Builder",
		WorkosDescription: conv.ToPGTextEmpty("After"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now.Add(time.Minute)),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	row, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosSlug:     "custom-builder",
	})
	require.NoError(t, err)
	require.Equal(t, "event_01SEED", row.WorkosLastEventID.String)

	replaced, err := accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "custom-builder",
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGText("event_02SEED"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	replaced, err = accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "custom-builder",
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now.Add(time.Minute)),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	assignments, err := accessrepo.New(ti.conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosUserID:   "user_1",
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.False(t, assignments[0].DeletedAt.Valid)
	require.Equal(t, "event_02SEED", assignments[0].WorkosLastEventID.String)
}

func TestRoleManager_ReplaceRoleAssignmentSoftDeletesPreviousRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", "member"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")

	// Use ReplaceOrganizationRoleAssignment directly to test its upsert-one + soft-delete-others behavior.
	now := time.Now().UTC()
	replaced, err := accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "member",
		UserID:             conv.ToPGTextEmpty("local_user_1"),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	replaced, err = accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "custom-builder",
		UserID:             conv.ToPGTextEmpty("local_user_1"),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	assignments, err := accessrepo.New(ti.conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosUserID:   "user_1",
	})
	require.NoError(t, err)
	activeCount := 0
	deletedCount := 0
	for _, assignment := range assignments {
		if assignment.DeletedAt.Valid {
			deletedCount++
			continue
		}
		activeCount++
	}
	require.Equal(t, 1, activeCount)
	require.Equal(t, 1, deletedCount)
}
