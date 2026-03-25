package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLoadGrants_loadsUserAndRoleGrants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)
	organizationID := "org_load_grants"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_admin")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, access.ScopeBuildRead, "proj:123")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, access.ScopeMCPConnect, "toolA")

	grants, err := access.LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = access.GrantsToContext(ctx, grants)
	require.NoError(t, access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: "proj:123"}))
	require.NoError(t, access.Require(ctx, access.Check{Scope: access.ScopeMCPConnect, ResourceID: "toolA"}))
}

func TestLoadGrants_rejectsEmptyOrganizationID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)

	grants, err := access.LoadGrants(ctx, conn, "", []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_123"),
	})
	require.Error(t, err)
	require.Nil(t, grants)
}

func TestLoadGrants_rejectsMissingPrincipals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)
	organizationID := "org_missing_principals"

	seedOrganization(t, ctx, conn, organizationID)

	grants, err := access.LoadGrants(ctx, conn, organizationID, nil)
	require.Error(t, err)
	require.Nil(t, grants)
}

func TestLoadGrants_rejectsInvalidPrincipal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)
	organizationID := "org_invalid_principal"

	seedOrganization(t, ctx, conn, organizationID)

	grants, err := access.LoadGrants(ctx, conn, organizationID, []urn.Principal{{}})
	require.Error(t, err)
	require.Nil(t, grants)
}

func TestLoadGrants_returnsEmptyGrantSetWhenNoRowsMatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)
	organizationID := "org_empty_grants"

	seedOrganization(t, ctx, conn, organizationID)

	grants, err := access.LoadGrants(ctx, conn, organizationID, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_123"),
	})
	require.NoError(t, err)

	ctx = access.GrantsToContext(ctx, grants)
	projectIDs, err := access.Filter(ctx, access.ScopeBuildRead, []string{"proj:123"})
	require.NoError(t, err)
	require.Empty(t, projectIDs)
}
