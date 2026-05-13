package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRequire_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_authz_require_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_require_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_require_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeProjectRead, WildcardResource)
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	err = engine.Require(ctx,
		Check{Scope: ScopeProjectRead, ResourceID: "proj:123"},
		Check{Scope: ScopeMCPConnect, ResourceID: "toolA"},
	)
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: "toolB"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestFilter_withLoadedGrantsFromContext(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_authz_filter_integration"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_filter_integration")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_filter_integration")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrant(t, ctx, conn, organizationID, userPrincipal, ScopeProjectRead, "proj:123")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolA")
	seedGrant(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, "toolB")

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{userPrincipal, rolePrincipal})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	projectIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeProjectRead, ResourceID: "proj:123"},
		{Scope: ScopeProjectRead, ResourceID: "proj:456"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"proj:123"}, projectIDs)

	toolIDs, err := engine.Filter(ctx, []Check{
		{Scope: ScopeMCPConnect, ResourceID: "toolA"},
		{Scope: ScopeMCPConnect, ResourceID: "toolB"},
		{Scope: ScopeMCPConnect, ResourceID: "toolC"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"toolA", "toolB"}, toolIDs)
}

func TestFilter_withDimensions(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_authz_filter_dims"
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_filter_dims")

	seedOrganization(t, ctx, conn, organizationID)
	seedGrantWithSelector(t, ctx, conn, organizationID, rolePrincipal, ScopeMCPConnect, Selector{
		"resource_kind": "mcp",
		"resource_id":   "toolsetX",
		"tool":          "allowed_tool",
	})

	grants, err := LoadGrants(ctx, conn, organizationID, []urn.Principal{rolePrincipal})
	require.NoError(t, err)

	ctx = GrantsToContext(ctx, grants)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, rbacAlwaysEnabled, challengeLoggingAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	results, err := engine.Filter(ctx, []Check{
		MCPToolCallCheck("toolsetX", MCPToolCallDimensions{Tool: "allowed_tool", Disposition: ""}),
		MCPToolCallCheck("toolsetX", MCPToolCallDimensions{Tool: "denied_tool", Disposition: ""}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"toolsetX"}, results)
}
