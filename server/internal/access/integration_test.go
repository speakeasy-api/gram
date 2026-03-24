package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRequire_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)
	organizationID := "org_access_require_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_require_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_require_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeBuildRead, WildcardResource)
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)

	err = Require(ctx,
		Check{Scope: ScopeBuildRead, ResourceID: "proj:123"},
		Check{Scope: ScopeMCPConnect, ResourceID: "toolA"},
	)
	require.NoError(t, err)

	err = Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: "toolB"})
	require.Error(t, err)
}

func TestFilter_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newTestDB(t)
	organizationID := "org_access_filter_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_filter_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_filter_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeBuildRead, "proj:123")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolB")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)

	projectIDs, err := Filter(ctx, ScopeBuildRead, []string{"proj:123", "proj:456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj:123"}, projectIDs)

	toolIDs, err := Filter(ctx, ScopeMCPConnect, []string{"toolA", "toolB", "toolC"})
	require.NoError(t, err)
	require.Equal(t, []string{"toolA", "toolB"}, toolIDs)
}
