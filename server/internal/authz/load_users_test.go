package authz

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type userGrantQueryCounter struct {
	accessrepo.DBTX
	queries int
}

func (db *userGrantQueryCounter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.queries++
	rows, err := db.DBTX.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("counted query: %w", err)
	}
	return rows, nil
}

func (db *userGrantQueryCounter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	db.queries++
	return db.DBTX.QueryRow(ctx, sql, args...)
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
	check := Check{Scope: ScopeMCPConnect, ResourceID: "direct-resource"}
	require.True(t, GrantsSatisfy(batch["owner_direct"], check))
	require.False(t, GrantsSatisfy(batch["owner_denied"], check), "another owner's direct grants must not leak")
	require.False(t, GrantsSatisfy(batch["owner_denied"], Check{Scope: ScopeMCPConnect, ResourceID: "other-org-resource"}))
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
