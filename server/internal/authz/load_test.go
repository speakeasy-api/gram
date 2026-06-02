package authz

import (
	"testing"

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
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient())
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

	grants, err := GrantsForRole(ctx, testenv.NewLogger(t), conn, organizationID, SystemRoleAdmin, "role:global:"+adminRole.ID.String())
	require.NoError(t, err)
	require.NotEmpty(t, grants)

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

	_, err = accessrepo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.ErrorIs(t, err, pgx.ErrNoRows)
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
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient())
	projectIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj:123"},
	})
	require.NoError(t, err)
	require.Empty(t, projectIDs)
}
