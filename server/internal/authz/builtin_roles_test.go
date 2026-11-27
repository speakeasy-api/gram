package authz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestEnsureLocalBuiltInGlobalRoles_idempotentAndRestoresDeleted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)

	require.NoError(t, EnsureLocalBuiltInGlobalRoles(ctx, conn))

	q := repo.New(conn)
	idsBySlug := make(map[string]string)
	for _, spec := range LocalBuiltInRoles() {
		row, err := q.GetGlobalRoleBySlug(ctx, spec.Slug)
		require.NoError(t, err)
		require.Equal(t, spec.Name, row.WorkosName)
		require.Equal(t, spec.Description, conv.FromPGTextOrEmpty[string](row.WorkosDescription))
		require.False(t, row.WorkosLastEventID.Valid, "local built-in roles carry no WorkOS event id")
		require.False(t, row.Deleted)
		require.False(t, row.WorkosDeleted)
		idsBySlug[spec.Slug] = row.ID.String()
	}

	// Soft-delete one role, then re-run EnsureLocalBuiltInGlobalRoles to confirm
	// it restores the row while preserving existing IDs.
	deletedAt := time.Now().UTC()
	_, err := q.MarkGlobalRoleDeleted(ctx, repo.MarkGlobalRoleDeletedParams{
		WorkosSlug:        SystemRoleStandardAdmin,
		WorkosDeletedAt:   conv.ToPGTimestamptz(deletedAt),
		WorkosLastEventID: conv.ToPGText("event_should_not_persist"),
	})
	require.NoError(t, err)

	require.NoError(t, EnsureLocalBuiltInGlobalRoles(ctx, conn))

	for _, spec := range LocalBuiltInRoles() {
		row, err := q.GetGlobalRoleBySlug(ctx, spec.Slug)
		require.NoError(t, err)
		require.Equal(t, idsBySlug[spec.Slug], row.ID.String())
		require.False(t, row.Deleted)
		require.False(t, row.WorkosDeleted)
	}
}

func TestLoadGrants_mergesStandardRoleGrantsWithStoredGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_test"
	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, EnsureLocalBuiltInGlobalRoles(ctx, conn))

	q := repo.New(conn)
	stdAdminRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleStandardAdmin)
	require.NoError(t, err)
	stdMemberRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleStandardMember)
	require.NoError(t, err)
	superAdminRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleSuperAdmin)
	require.NoError(t, err)

	stdAdminPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+stdAdminRole.ID.String())
	stdMemberPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+stdMemberRole.ID.String())
	superAdminPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+superAdminRole.ID.String())

	// Store a non-role grant (risk policy) on Standard Admin to confirm stored
	// rows stay merged alongside code-defined scopes.
	seedGrant(t, ctx, conn, organizationID, stdAdminPrincipal, ScopeRiskPolicyEvaluate, "policy-1")

	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	adminGrants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{stdAdminPrincipal})
	require.NoError(t, err)
	adminCtx := GrantsToContext(ctx, adminGrants)
	require.NoError(t, engine.Require(adminCtx, Org(OrgIDFrom(organizationID)).Admin()))
	require.NoError(t, engine.Require(adminCtx, MCPServer(MCPServerIDFrom("srv-1"), ProjectIDFrom(testProjectID)).Write()))
	require.NoError(t, engine.Require(adminCtx, Environment(EnvironmentIDFrom("env-1"), ProjectIDFrom(testProjectID)).Write()))
	require.Error(t, engine.Require(adminCtx, Chats(ProjectIDFrom(testProjectID)).Read()), "private content stays excluded from Standard Admin defaults")
	require.True(t, GrantsSatisfy(adminGrants, RiskPolicyEvaluateCheck("policy-1")))

	memberGrants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{stdMemberPrincipal})
	require.NoError(t, err)
	memberCtx := GrantsToContext(ctx, memberGrants)
	require.NoError(t, engine.Require(memberCtx, MCPServer(MCPServerIDFrom("srv-1"), ProjectIDFrom(testProjectID)).Connect()))
	require.NoError(t, engine.Require(memberCtx, Skill(SkillIDFrom("skill-1")).Read()))
	require.Error(t, engine.Require(memberCtx, Org(OrgIDFrom(organizationID)).Admin()))
	require.Error(t, engine.Require(memberCtx, Environment(EnvironmentIDFrom("env-1"), ProjectIDFrom(testProjectID)).Read()))

	superGrants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{superAdminPrincipal})
	require.NoError(t, err)
	require.Empty(t, superGrants, "Super Admin carries no static code-defined grants")
}

func TestGrantsForRoleTxAndScopeCoverageTx_includeStandardRoles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_code_defined_coverage"
	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, EnsureLocalBuiltInGlobalRoles(ctx, conn))

	q := repo.New(conn)
	stdAdminRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleStandardAdmin)
	require.NoError(t, err)
	stdMemberRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleStandardMember)
	require.NoError(t, err)

	stdAdminURN := "role:global:" + stdAdminRole.ID.String()
	stdMemberURN := "role:global:" + stdMemberRole.ID.String()

	scoped, err := GrantsForRoleTx(ctx, conn, organizationID, stdAdminURN)
	require.NoError(t, err)
	gotScopes := make([]Scope, 0, len(scoped))
	for _, g := range scoped {
		gotScopes = append(gotScopes, Scope(g.Scope))
		require.Len(t, g.Selectors, 1)
		require.True(t, g.Selectors[0].CoversEveryResource(), "standard role grant %s must cover every resource of its kind", g.Scope)
	}
	require.ElementsMatch(t, StandardRoleScopes(SystemRoleStandardAdmin), gotScopes)

	coverage, err := ScopeCoverageTx(ctx, conn, organizationID, []string{stdAdminURN, stdMemberURN})
	require.NoError(t, err)

	widened := SensitiveScopesWidened(coverage[stdMemberURN], coverage[stdAdminURN])
	require.Contains(t, widened, string(ScopeOrgAdmin))
	require.NotContains(t, widened, string(ScopeChatRead))
	require.Contains(t, coverage[stdAdminURN], ScopeCoverage{Scope: ScopeEnvironmentRead, Unrestricted: true, Resources: nil})
	require.NotContains(t, coverage[stdMemberURN], ScopeCoverage{Scope: ScopeEnvironmentRead, Unrestricted: true, Resources: nil})
}

func TestStandardRoleScopesFor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	require.NoError(t, EnsureLocalBuiltInGlobalRoles(ctx, conn))

	q := repo.New(conn)
	stdMemberRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleStandardMember)
	require.NoError(t, err)
	superAdminRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleSuperAdmin)
	require.NoError(t, err)

	scopes, err := StandardRoleScopesFor(ctx, conn, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_1"),
		urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+stdMemberRole.ID.String()),
		urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+superAdminRole.ID.String()),
	})
	require.NoError(t, err)
	require.Contains(t, scopes, string(ScopeMCPConnect))
	require.Contains(t, scopes, string(ScopeSkillRead))
	require.NotContains(t, scopes, string(ScopeOrgAdmin))

	// A non-global principal list requires no global role lookup and returns empty.
	empty, err := StandardRoleScopesFor(ctx, conn, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_1"),
	})
	require.NoError(t, err)
	require.Empty(t, empty)
}
