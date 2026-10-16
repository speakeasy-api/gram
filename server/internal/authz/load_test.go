package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLoadGrants_loadsUserAndRoleGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_load_grants"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_admin")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeProjectRead, "proj:123")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "proj:123"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: "toolA"}))
}

func TestSeedSystemRoleGrantsBootstrapsGlobalRoles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_seed_system_roles"
	seedOrganization(t, ctx, conn, organizationID)

	err := SeedSystemRoleGrants(ctx, conn, organizationID)
	require.NoError(t, err)

	adminRole, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	require.Equal(t, "Admin", adminRole.WorkosName)

	grants, err := GrantsForRole(ctx, testenv.NewLogger(t), conn, organizationID, "role:global:"+adminRole.ID.String())
	require.NoError(t, err)
	require.NotEmpty(t, grants)

	seededScopes := make([]Scope, 0, len(grants))
	for _, grant := range grants {
		seededScopes = append(seededScopes, Scope(grant.Scope))
	}
	for _, scope := range []Scope{ScopeAgentRead, ScopeAgentWrite, ScopeAgentAuthorize, ScopeAgentTransfer} {
		require.Contains(t, seededScopes, scope, "new Admin role must contain explicit %s grant", scope)
	}

	q := accessrepo.New(conn)
	adminPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+adminRole.ID.String())
	adminRows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   adminPrincipal.String(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, adminRows)
	_, err = q.DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{
		ID:             adminRows[0].ID,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	seedGrant(t, ctx, conn, organizationID, adminPrincipal, ScopeRiskPolicyEvaluate, "policy-1")
	rowsBeforeReseed, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   adminPrincipal.String(),
	})
	require.NoError(t, err)

	err = SeedSystemRoleGrants(ctx, conn, organizationID)
	require.NoError(t, err)

	rows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   adminPrincipal.String(),
	})
	require.NoError(t, err)
	require.Len(t, rows, len(rowsBeforeReseed))

	scopes := make([]string, 0, len(rows))
	for _, row := range rows {
		scopes = append(scopes, row.Scope)
	}
	require.Contains(t, scopes, string(ScopeRiskPolicyEvaluate))
}

func TestRemovingSystemRoleCreationEligibilityIsDurable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org-create-defaults"
	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, organizationID))
	q := accessrepo.New(conn)
	for _, slug := range []string{SystemRoleAdmin, SystemRoleMember} {
		role, err := q.GetGlobalRoleBySlug(ctx, slug)
		require.NoError(t, err)
		principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
		rows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{OrganizationID: organizationID, PrincipalUrn: principal.String()})
		require.NoError(t, err)
		seededCreate := false
		for _, row := range rows {
			seededCreate = seededCreate || row.Scope == string(ScopeAgentWrite)
		}
		require.Equal(t, slug == SystemRoleAdmin, seededCreate, "persisted seed grants must give creation only to Admin")
		for _, row := range rows {
			if row.Scope == string(ScopeAgentWrite) {
				_, err := q.DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{OrganizationID: organizationID, ID: row.ID})
				require.NoError(t, err)
			}
		}
		grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{principal})
		require.NoError(t, err)
		allowed := GrantsContainSelector(grants, ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, "employee"))
		require.False(t, allowed, "deleting persisted creation grants must revoke eligibility, including for Admin")
		listed, err := GrantsForRole(ctx, testenv.NewLogger(t), conn, organizationID, principal.String())
		require.NoError(t, err)
		found := false
		for _, grant := range listed {
			found = found || grant.Scope == string(ScopeAgentWrite)
		}
		require.Equal(t, allowed, found, "role editor and enforcement agree")
	}
}

func TestSystemRoleDefaultsSkipsLookupWithPersistedAssistantGrant(t *testing.T) {
	t.Parallel()
	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+uuid.NewString())
	grant := NewGrant(ScopeAssistantWrite, WildcardResource)
	grant.PrincipalUrn = principal.String()
	// A nil database proves that a Member-like grant set with no creation
	// permission does not need a global-role query.
	defaults, err := systemRoleDefaults(t.Context(), nil, []urn.Principal{principal}, []Grant{grant})
	require.NoError(t, err)
	require.Empty(t, defaults)
}

func TestSeedSystemRoleGrantsRollsBackOnGrantFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)

	err := SeedSystemRoleGrants(ctx, conn, "org_missing")
	require.Error(t, err)

	for _, slug := range []string{SystemRoleAdmin, SystemRoleMember} {
		_, err = accessrepo.New(conn).GetGlobalRoleBySlug(ctx, slug)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
}

func TestLoadGrants_rejectsEmptyOrganizationID(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)

	grants, err := LoadGrants(t.Context(), conn, "", []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_123"),
	})
	require.Error(t, err)
	require.Nil(t, grants)
}

func TestLoadGrants_rejectsMissingPrincipals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	conn := newTestDB(t)
	organizationID := "org_missing_principals"

	seedOrganization(t, ctx, conn, organizationID)

	grants, err := LoadGrants(ctx, conn, organizationID, nil)
	require.Error(t, err)
	require.Nil(t, grants)
}

func TestLoadGrants_rejectsInvalidPrincipal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_invalid_principal"

	seedOrganization(t, ctx, conn, organizationID)

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{{}})
	require.Error(t, err)
	require.Nil(t, grants)
}

func TestLoadGrants_returnsEmptyGrantSetWhenNoRowsMatch(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_empty_grants"

	seedOrganization(t, ctx, conn, organizationID)

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_123"),
	})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())
	projectIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj:123"},
	})
	require.NoError(t, err)
	require.Empty(t, projectIDs)
}

func TestLoadGrantsAddsAssistantDefaultsOnlyForCanonicalSystemRole(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_canonical_assistant_defaults"
	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, organizationID))

	q := accessrepo.New(conn)
	adminRole, err := q.GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	admin := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+adminRole.ID.String())
	_, err = q.DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: organizationID,
		PrincipalUrn:   admin,
	})
	require.NoError(t, err)

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{admin})
	require.NoError(t, err)
	require.True(t, GrantsSatisfy(grants, AssistantCheck(ScopeAssistantRead, "assistant_1", "project_a")))
	require.True(t, GrantsSatisfy(grants, AssistantCheck(ScopeAssistantWrite, "assistant_1", "project_a")))

	grants, err = LoadGrants(ctx, conn, organizationID, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString()),
	})
	require.NoError(t, err)
	require.False(t, GrantsSatisfy(grants, AssistantCheck(ScopeAssistantRead, "assistant_1", "project_a")))
}
