package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRequire_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := enterpriseCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_access_require_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_require_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_require_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, access.ScopeBuildRead, access.WildcardResource)
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, access.ScopeMCPConnect, "toolA")

	grants, err := access.LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = access.GrantsToContext(ctx, grants)

	err = access.Require(ctx,
		access.Check{Scope: access.ScopeBuildRead, ResourceID: "proj:123"},
		access.Check{Scope: access.ScopeMCPConnect, ResourceID: "toolA"},
	)
	require.NoError(t, err)

	err = access.Require(ctx, access.Check{Scope: access.ScopeMCPConnect, ResourceID: "toolB"})
	require.Error(t, err)
}

func TestFilter_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := enterpriseCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_access_filter_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_filter_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_filter_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, access.ScopeBuildRead, "proj:123")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, access.ScopeMCPConnect, "toolA")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, access.ScopeMCPConnect, "toolB")

	grants, err := access.LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = access.GrantsToContext(ctx, grants)

	projectIDs, err := access.Filter(ctx, access.ScopeBuildRead, []string{"proj:123", "proj:456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj:123"}, projectIDs)

	toolIDs, err := access.Filter(ctx, access.ScopeMCPConnect, []string{"toolA", "toolB", "toolC"})
	require.NoError(t, err)
	require.Equal(t, []string{"toolA", "toolB"}, toolIDs)
}
