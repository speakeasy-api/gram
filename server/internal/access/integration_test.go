package access

import (
	"testing"

	trequire "github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRequire_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_access_require_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_require_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_require_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeBuildRead, WildcardResource)
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	trequire.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	manager := NewManager(testLogger(t), conn, stubFeatureChecker{enabled: true})

	err = manager.Require(ctx,
		Check{Scope: ScopeBuildRead, ResourceID: "proj:123"},
		Check{Scope: ScopeMCPConnect, ResourceID: "toolA"},
	)
	trequire.NoError(t, err)

	err = manager.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: "toolB"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestFilter_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_access_filter_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_filter_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_filter_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeBuildRead, "proj:123")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolB")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	trequire.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	manager := NewManager(testLogger(t), conn, stubFeatureChecker{enabled: true})

	projectIDs, err := manager.Filter(ctx, ScopeBuildRead, []string{"proj:123", "proj:456"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"proj:123"}, projectIDs)

	toolIDs, err := manager.Filter(ctx, ScopeMCPConnect, []string{"toolA", "toolB", "toolC"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"toolA", "toolB"}, toolIDs)
}
