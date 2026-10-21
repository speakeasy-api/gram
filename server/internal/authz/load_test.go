package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

func TestLoadGrantsHonorsRevokedSystemRoleAssistantGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_revoked_assistant_grants"
	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, organizationID))
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())
	assistantRead := AssistantCheck(ScopeAssistantRead, "assistant_1", "project_a")
	assistantWrite := AssistantCheck(ScopeAssistantWrite, "assistant_1", "project_a")

	q := accessrepo.New(conn)
	for _, tc := range []struct {
		slug   string
		checks []Check
	}{
		{slug: SystemRoleMember, checks: []Check{assistantRead}},
		{slug: SystemRoleAdmin, checks: []Check{assistantRead, assistantWrite}},
	} {
		role, err := q.GetGlobalRoleBySlug(ctx, tc.slug)
		require.NoError(t, err)
		principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())

		grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{principal})
		require.NoError(t, err)
		for _, check := range tc.checks {
			require.NoError(t, engine.Require(GrantsToContext(ctx, grants), check), "%s role must hold seeded %s", tc.slug, check.Scope)
		}

		_, err = PatchRoleGrantsTx(ctx, conn, organizationID, tc.slug, principal.String(), nil, SystemRoleAssistantGrants(tc.slug))
		require.NoError(t, err)

		grants, err = LoadGrants(ctx, conn, organizationID, []urn.Principal{principal})
		require.NoError(t, err)
		require.NotEmpty(t, grants, "%s role keeps its other grants", tc.slug)
		for _, grant := range grants {
			require.NotEqual(t, ResourceKindAssistant, ResourceKindForScope(grant.Scope), "%s role must not regain %s", tc.slug, grant.Scope)
		}
		for _, check := range tc.checks {
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, engine.Require(GrantsToContext(ctx, grants), check), &oopsErr, "%s role must lose revoked %s", tc.slug, check.Scope)
			require.Equal(t, oops.CodeForbidden, oopsErr.Code)
		}
	}

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString()),
	})
	require.NoError(t, err)
	require.False(t, GrantsSatisfy(grants, assistantRead))
}

func TestSystemRoleAssistantGrants(t *testing.T) {
	t.Parallel()

	scopes := func(grants []*RoleGrant) []string {
		out := make([]string, 0, len(grants))
		for _, grant := range grants {
			require.Nil(t, grant.Selectors)
			out = append(out, grant.Scope)
		}
		return out
	}
	require.ElementsMatch(t, []string{string(ScopeAssistantRead), string(ScopeAssistantWrite)}, scopes(SystemRoleAssistantGrants(SystemRoleAdmin)))
	require.ElementsMatch(t, []string{string(ScopeAssistantRead)}, scopes(SystemRoleAssistantGrants(SystemRoleMember)))
	require.Empty(t, SystemRoleAssistantGrants("org-custom"))
}
