package access

import (
	"testing"

	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRoleManager_ListRolesHidesLocalBuiltInRolesUntilMigration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	require.NoError(t, authz.EnsureLocalBuiltInGlobalRoles(ctx, ti.conn))
	seedGlobalRole(t, ctx, ti.conn, mockSystemRole("role_admin", "Admin", authz.SystemRoleAdmin))
	seedGlobalRole(t, ctx, ti.conn, mockSystemRole("role_member", "Member", authz.SystemRoleMember))

	listed, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{SessionToken: nil})
	require.NoError(t, err)

	slugs := make([]string, 0, len(listed.Roles))
	for _, role := range listed.Roles {
		slugs = append(slugs, role.Slug)
		if role.Slug == authz.SystemRoleAdmin || role.Slug == authz.SystemRoleMember {
			require.True(t, role.IsSystem)
			require.False(t, role.LockedGrants, "legacy system role %s must remain unlocked before migration", role.Slug)
		}
	}
	require.Contains(t, slugs, authz.SystemRoleAdmin)
	require.Contains(t, slugs, authz.SystemRoleMember)
	require.NotContains(t, slugs, authz.SystemRoleSuperAdmin)
	require.NotContains(t, slugs, authz.SystemRoleStandardAdmin)
	require.NotContains(t, slugs, authz.SystemRoleStandardMember)
}

func TestRoleManager_GetRoleByIDReturnsStandardRoleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	require.NoError(t, authz.EnsureLocalBuiltInGlobalRoles(ctx, ti.conn))

	q := accessrepo.New(ti.conn)
	stdAdmin, err := q.GetGlobalRoleBySlug(ctx, authz.SystemRoleStandardAdmin)
	require.NoError(t, err)

	role, err := ti.service.roleMgr.GetRoleByID(ctx, orgID, stdAdmin.ID.String())
	require.NoError(t, err)
	require.Equal(t, authz.SystemRoleStandardAdmin, role.Slug)
	require.True(t, role.IsSystem)
	require.True(t, role.LockedGrants)

	gotScopes := make([]authz.Scope, 0, len(role.Grants))
	for _, grant := range role.Grants {
		gotScopes = append(gotScopes, authz.Scope(grant.Scope))
	}
	require.ElementsMatch(t, authz.StandardRoleScopes(authz.SystemRoleStandardAdmin), gotScopes)

	tx := testenv.BeginTx(t, ctx, ti.conn)

	txRole, err := ti.service.roleMgr.GetRoleByIDTx(ctx, tx, orgID, stdAdmin.ID.String())
	require.NoError(t, err)
	require.Len(t, txRole.Grants, len(role.Grants))
}

func TestService_ListIdentityAccess_ResolvesStandardRolesAndHonorsBlocks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	require.NoError(t, authz.EnsureLocalBuiltInGlobalRoles(ctx, ti.conn))

	openServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	blockedServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	openSkill := seedSkill(t, ctx, ti.conn, orgID)
	blockedSkill := seedSkill(t, ctx, ti.conn, orgID)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_std_member", "std-member@test.com", "Standard Member User", "workos_std_member", "membership_std_member")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_std_member", mockMember("", "membership_std_member", "workos_std_member", authz.SystemRoleStandardMember))

	// Direct blocks must still withdraw access granted by a code-defined role.
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_std_member")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPBlockedConnect, blockedServer)
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPBlockedRead, blockedServer)
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeSkillBlockedRead, blockedSkill)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_std_member", SessionToken: nil})
	require.NoError(t, err)

	serverIDs := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		serverIDs = append(serverIDs, server.ID)
	}
	require.ElementsMatch(t, []string{openServer}, serverIDs)
	require.ElementsMatch(t, []string{openSkill}, listedSkillIDs(result))
}

func TestRecordStandardRoleAudience(t *testing.T) {
	t.Parallel()

	stdAdminURN := "role:global:" + uuid.NewString()
	stdMemberURN := "role:global:" + uuid.NewString()
	superAdminURN := "role:global:" + uuid.NewString()
	adminURN := "role:global:" + uuid.NewString()

	roles := []accessrepo.ListActiveOrganizationRolesRow{
		{RoleUrn: stdAdminURN, WorkosSlug: authz.SystemRoleStandardAdmin},
		{RoleUrn: stdMemberURN, WorkosSlug: authz.SystemRoleStandardMember},
		{RoleUrn: superAdminURN, WorkosSlug: authz.SystemRoleSuperAdmin},
		{RoleUrn: adminURN, WorkosSlug: authz.SystemRoleAdmin},
	}

	recorded := make(map[string][]string)
	recordStandardRoleAudience(roles, func(principalURN, level, appliesTo, tool, disposition string) {
		require.Equal(t, audienceAppliesToAllResources, appliesTo)
		require.Empty(t, tool)
		require.Empty(t, disposition)
		recorded[principalURN] = append(recorded[principalURN], level)
	})

	require.ElementsMatch(t, []string{audienceLevelUse, audienceLevelView, audienceLevelManage}, recorded[stdAdminURN])
	require.ElementsMatch(t, []string{audienceLevelUse, audienceLevelView}, recorded[stdMemberURN])
	require.Empty(t, recorded[superAdminURN])
	require.Empty(t, recorded[adminURN])
}

func TestLockedBuiltInRoles_RejectPermissionMutations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	require.NoError(t, authz.EnsureLocalBuiltInGlobalRoles(ctx, ti.conn))
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	audienceVersion := currentAudienceVersion(t, ctx, ti, serverID)

	q := accessrepo.New(ti.conn)
	newName := "Renamed Built-in"

	for _, spec := range authz.LocalBuiltInRoles() {
		row, err := q.GetGlobalRoleBySlug(ctx, spec.Slug)
		require.NoError(t, err)
		roleID := row.ID.String()
		roleURN := "role:global:" + roleID

		roleView, err := ti.service.roleMgr.GetRoleByID(ctx, orgID, roleID)
		require.NoError(t, err)
		require.True(t, roleView.IsSystem, "%s must report is_system=true", spec.Slug)
		require.True(t, roleView.LockedGrants, "%s must report locked_grants=true", spec.Slug)

		// UpdateRole rejects adding grants.
		_, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
			ID:        roleID,
			AddGrants: []*gen.RoleGrant{{Scope: string(authz.ScopeChatRead), Selectors: nil}},
		})
		require.Error(t, err, "%s must reject AddGrants", spec.Slug)
		require.ErrorIs(t, err, authz.ErrLockedRoleGrants)
		var se *oops.ShareableError
		require.ErrorAs(t, err, &se)
		require.Equal(t, oops.CodeBadRequest, se.Code)

		// UpdateRole rejects removing grants.
		_, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
			ID:           roleID,
			RemoveGrants: []*gen.RoleGrant{{Scope: string(authz.ScopeMCPConnect), Selectors: nil}},
		})
		require.Error(t, err, "%s must reject RemoveGrants", spec.Slug)
		require.ErrorIs(t, err, authz.ErrLockedRoleGrants)
		se = nil
		require.ErrorAs(t, err, &se)
		require.Equal(t, oops.CodeBadRequest, se.Code)

		// UpdateRole rejects name/description changes.
		_, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
			ID:   roleID,
			Name: &newName,
		})
		require.Error(t, err, "%s must reject Name changes", spec.Slug)
		se = nil
		require.ErrorAs(t, err, &se)
		require.Equal(t, oops.CodeBadRequest, se.Code)

		// DeleteRole rejects all locked built-in roles.
		err = ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: roleID})
		require.Error(t, err, "%s must reject DeleteRole", spec.Slug)
		se = nil
		require.ErrorAs(t, err, &se)
		require.Equal(t, oops.CodeBadRequest, se.Code)

		// SetResourceAudience rejects per-server rules targeting locked built-in roles.
		_, err = ti.service.SetResourceAudience(ctx, &gen.SetResourceAudiencePayload{
			ResourceKind:    "mcp",
			ResourceID:      serverID,
			ExpectedVersion: audienceVersion,
			Entries: []*gen.SetResourceAudienceEntry{
				{PrincipalUrn: roleURN, Level: audienceLevelBlocked},
			},
		})
		require.Error(t, err, "%s must reject SetResourceAudience", spec.Slug)
		require.ErrorIs(t, err, authz.ErrLockedRoleGrants)
		se = nil
		require.ErrorAs(t, err, &se)
		require.Equal(t, oops.CodeBadRequest, se.Code)

		// CreateTemporaryGrant rejects temporary grants targeting locked built-in roles.
		_, err = ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{
			PrincipalUrn: roleURN,
			Scope:        string(authz.ScopeChatRead),
			ExpiresAt:    inOneHour(),
		})
		require.Error(t, err, "%s must reject CreateTemporaryGrant", spec.Slug)
		require.ErrorIs(t, err, authz.ErrLockedRoleGrants)
		se = nil
		require.ErrorAs(t, err, &se)
		require.Equal(t, oops.CodeBadRequest, se.Code)
	}

	// Membership-only updates on a locked standard role remain allowed even when
	// callers send empty grant slices.
	stdMemberRow, err := q.GetGlobalRoleBySlug(ctx, authz.SystemRoleStandardMember)
	require.NoError(t, err)
	seedGlobalRole(t, ctx, ti.conn, mockSystemRole("role_member", "Member", authz.SystemRoleMember))
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_locked_member", "locked-member@test.com", "Locked Member", "workos_locked_member", "membership_locked_member")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_locked_member", mockMember(mockidp.MockOrgID, "membership_locked_member", "workos_locked_member", authz.SystemRoleMember))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_locked_member", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_locked_member",
		UserID:         "workos_locked_member",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{authz.SystemRoleMember},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:           stdMemberRow.ID.String(),
		AddGrants:    []*gen.RoleGrant{},
		RemoveGrants: []*gen.RoleGrant{},
		MemberIds:    []string{"local_locked_member"},
	})
	require.NoError(t, err)
	require.True(t, updated.LockedGrants)
	require.Equal(t, 1, updated.MemberCount)
}
