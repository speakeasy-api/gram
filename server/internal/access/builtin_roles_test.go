package access

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
