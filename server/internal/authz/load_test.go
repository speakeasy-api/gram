package authz

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	require.NoError(t, engine.Require(ctx, MCPServer(MCPServerIDFrom("toolA"), ProjectIDFrom(testProjectID)).Connect()))
}

func TestSeedSystemRoleGrantsBootstrapsGlobalRoles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_seed_system_roles"
	seedOrganization(t, ctx, conn, organizationID)

	err := SeedSystemRoleGrants(ctx, conn, organizationID)
	require.NoError(t, err)

	adminRole, err := repo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
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

	q := repo.New(conn)
	adminPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+adminRole.ID.String())
	adminRows, err := q.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   adminPrincipal.String(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, adminRows)
	_, err = q.DeletePrincipalGrant(ctx, repo.DeletePrincipalGrantParams{
		ID:             adminRows[0].ID,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	seedGrant(t, ctx, conn, organizationID, adminPrincipal, ScopeRiskPolicyEvaluate, "policy-1")
	rowsBeforeReseed, err := q.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   adminPrincipal.String(),
	})
	require.NoError(t, err)

	err = SeedSystemRoleGrants(ctx, conn, organizationID)
	require.NoError(t, err)

	rows, err := q.ListPrincipalGrantsByOrg(ctx, repo.ListPrincipalGrantsByOrgParams{
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
		_, err = repo.New(conn).GetGlobalRoleBySlug(ctx, slug)
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
	assistantRead := Assistant(AssistantIDFrom("assistant_1"), ProjectIDFrom("project_a")).Read()
	assistantWrite := Assistant(AssistantIDFrom("assistant_1"), ProjectIDFrom("project_a")).Write()

	q := repo.New(conn)
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

func TestLoadUserGrants_matchesIndividualPolicies(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestDB(t)
	const orgID = "org_batch_owners"
	const otherOrgID = "org_batch_other"
	seedOrganization(t, ctx, conn, orgID)
	seedOrganization(t, ctx, conn, otherOrgID)
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, orgID))
	for _, id := range []string{"owner_direct", "owner_role", "owner_shared_role", "owner_denied", "owner_removed", "owner_deleted"} {
		seedActiveOrganizationUser(t, ctx, conn, orgID, id)
	}
	seedActiveOrganizationUser(t, ctx, conn, otherOrgID, "owner_cross_org")
	for _, id := range []string{"owner_role", "owner_shared_role", "owner_removed", "owner_deleted"} {
		seedRoleAssignmentForUser(t, ctx, conn, orgID, id, SystemRoleMember)
	}
	seedGrant(t, ctx, conn, orgID, AllUsersPrincipal(), ScopeProjectRead, "shared-project")
	seedGrant(t, ctx, conn, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, "owner_direct"), ScopeMCPConnect, "direct-resource")
	seedGrant(t, ctx, conn, otherOrgID, urn.NewPrincipal(urn.PrincipalTypeUser, "owner_denied"), ScopeMCPConnect, "other-org-resource")
	err := orgrepo.New(conn).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: conv.ToPGText("owner_removed")})
	require.NoError(t, err)
	err = testrepo.New(conn).ForceSoftDeleteUser(ctx, "owner_deleted")
	require.NoError(t, err)

	ids := []string{"owner_direct", "owner_role", "owner_shared_role", "owner_denied", "owner_removed", "owner_deleted", "owner_cross_org", "missing", "", "owner_direct"}
	counted := &userGrantQueryCounter{DBTX: conn}
	batch, err := LoadUserGrants(ctx, counted, orgID, ids)
	require.NoError(t, err)
	require.Equal(t, 2, counted.queries, "principal and grant queries must not scale with owner count")
	for _, id := range ids {
		principals, err := ResolveUserPrincipals(ctx, conn, orgID, id)
		require.NoError(t, err)
		individual, err := LoadGrants(ctx, conn, orgID, principals)
		require.NoError(t, err)
		require.ElementsMatch(t, individual, batch[id], "owner %q", id)
	}
	check := MCPServer(MCPServerIDFrom("direct-resource"), ProjectIDFrom(testProjectID)).Connect()
	require.True(t, GrantsSatisfy(batch["owner_direct"], check))
	require.False(t, GrantsSatisfy(batch["owner_denied"], check), "another owner's direct grants must not leak")
	require.False(t, GrantsSatisfy(batch["owner_denied"], MCPServer(MCPServerIDFrom("other-org-resource"), ProjectIDFrom(testProjectID)).Connect()))
}

func TestLoadUserGrants_emptyAndInvalidInputsDoNotQuery(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	grants, err := LoadUserGrants(ctx, nil, "org_batch", nil)
	require.NoError(t, err)
	require.Empty(t, grants)
	_, err = LoadUserGrants(ctx, nil, "", []string{"owner"})
	require.Error(t, err)
	_, err = LoadUserGrants(ctx, nil, "org_batch", []string{urn.AllUsersPrincipalID})
	require.ErrorIs(t, err, ErrPrincipalInvalid)
}

func TestProvisionOrganizationAdminWithoutWorkOSUserIDReturnsError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_provision_without_workos_user"
	seedOrganization(t, ctx, conn, organizationID)

	provisioner := NewProvisioner(conn)
	acquireCount := conn.Stat().AcquireCount()
	err := provisioner.ProvisionOrganizationAdmin(ctx, organizationID, InitialOrganizationAdmin{
		UserID:             "user_test",
		WorkOSUserID:       "",
		WorkOSMembershipID: "",
	})
	require.EqualError(t, err, "assign initial organization admin: WorkOS user ID is required")
	require.Equal(t, acquireCount, conn.Stat().AcquireCount())

	_, err = repo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProvisionOrganizationAdminTxUsesCallerTransaction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_provision_transaction"
	workosUserID := "workos_user_transaction"
	seedOrganization(t, ctx, conn, organizationID)
	seedConnectedUser(t, ctx, conn, organizationID, "user_transaction", "user@example.com", "Test User", workosUserID, "membership_transaction")

	tx := testenv.BeginTx(t, ctx, conn)

	provisioner := NewProvisioner(conn)
	err := provisioner.ProvisionOrganizationAdminTx(ctx, tx, organizationID, InitialOrganizationAdmin{
		UserID:             "user_transaction",
		WorkOSUserID:       workosUserID,
		WorkOSMembershipID: "membership_transaction",
	})
	require.NoError(t, err)

	adminRole, err := repo.New(tx).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	grants, err := repo.New(conn).GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  []string{"role:global:" + adminRole.ID.String()},
	})
	require.NoError(t, err)
	require.Empty(t, grants)

	assignments, err := repo.New(conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, repo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Empty(t, assignments)
}

type userGrantQueryCounter struct {
	repo.DBTX
	queries int
}

func (db *userGrantQueryCounter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.queries++
	rows, err := db.DBTX.Query(ctx, sql, args...) //nolint:glint // notestingrawsql: wrapper forwards SQLc-generated SQL while counting database round trips
	if err != nil {
		return nil, fmt.Errorf("counted query: %w", err)
	}
	return rows, nil
}

func (db *userGrantQueryCounter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.queries++
	return db.DBTX.QueryRow(ctx, sql, args...) //nolint:glint // notestingrawsql: wrapper forwards SQLc-generated SQL while counting database round trips
}
