package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListIdentityAccess_DirectGrantOutranksRoleBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	lockedServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	otherLockedServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	openServer := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_staff", "Staff", "staff", "Directory-synced staff"))
	rolePrincipal := seededRolePrincipal(t, ctx, ti.conn, orgID, "staff")
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeMCPConnect, authz.WildcardResource)
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeMCPBlockedConnect, lockedServer)
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeMCPBlockedConnect, otherLockedServer)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_staff_admin", "staff-admin@test.com", "Staff Admin", "workos_staff_admin", "membership_staff_admin")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_staff_admin", mockMember("", "membership_staff_admin", "workos_staff_admin", "staff"))
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_staff_admin")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPConnect, lockedServer)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_staff_admin", SessionToken: nil})
	require.NoError(t, err)

	serverIDs := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		serverIDs = append(serverIDs, server.ID)
	}
	require.ElementsMatch(t, []string{lockedServer, openServer}, serverIDs, "the direct grant outranks the role's block on its own server only")
}

func TestService_ListIdentityAccess_OwnBlockOutranksOwnGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	server := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_blocked", "blocked@test.com", "Blocked", "workos_blocked", "membership_blocked")
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_blocked")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPConnect, server)
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeMCPBlockedConnect, server)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_blocked", SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, result.Servers)
}

func seedSkill(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) string {
	t.Helper()

	projectID := seedProject(t, ctx, conn, organizationID)
	suffix := uuid.NewString()[:8]
	skill, err := skillsrepo.New(conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID:   projectID,
		Name:        "skill-" + suffix,
		DisplayName: "Skill " + suffix,
		Summary:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return skill.ID.String()
}

func listedSkillIDs(result *gen.ListIdentityAccessResult) []string {
	ids := make([]string, 0, len(result.Skills))
	for _, skill := range result.Skills {
		ids = append(ids, skill.ID)
	}
	return ids
}

// A read block also withdraws write access, as it does in the engine, so a
// skill:write grant does not keep a read-blocked skill listed.
func TestService_ListIdentityAccess_SkillReadBlockWithdrawsWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	blockedSkill := seedSkill(t, ctx, ti.conn, orgID)
	openSkill := seedSkill(t, ctx, ti.conn, orgID)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_skill_writer", "skill-writer@test.com", "Skill Writer", "workos_skill_writer", "membership_skill_writer")
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_skill_writer")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeSkillWrite, authz.WildcardResource)
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeSkillBlockedRead, blockedSkill)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_skill_writer", SessionToken: nil})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{openSkill}, listedSkillIDs(result))
}

// A write block leaves read access in place, as it does in the engine.
func TestService_ListIdentityAccess_SkillWriteBlockLeavesReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	skill := seedSkill(t, ctx, ti.conn, orgID)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_skill_reader", "skill-reader@test.com", "Skill Reader", "workos_skill_reader", "membership_skill_reader")
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "local_skill_reader")
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeSkillRead, authz.WildcardResource)
	seedGrant(t, ctx, ti.conn, orgID, userPrincipal, authz.ScopeSkillBlockedWrite, skill)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: "local_skill_reader", SessionToken: nil})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{skill}, listedSkillIDs(result))
}
