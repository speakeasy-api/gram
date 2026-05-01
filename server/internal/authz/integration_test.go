package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

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
	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

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
	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	results, err := engine.Filter(ctx, []Check{
		MCPToolCallCheck("toolsetX", MCPToolCallDimensions{Tool: "allowed_tool", Disposition: ""}),
		MCPToolCallCheck("toolsetX", MCPToolCallDimensions{Tool: "denied_tool", Disposition: ""}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"toolsetX"}, results)
}

// TestAPIKeyPrincipal_PrepareAndEnforce covers the full RBAC path for
// plugin-scoped api-key requests (rfc-plugin-scoped-keys.md):
//
//   - PrepareContext loads grants from principal_grants for the api_key
//     principal when system_managed=true; with grants present,
//     ShouldEnforce flips on and Require enforces selector matches.
//   - When system_managed=false, no grants are loaded and Require returns
//     nil (legacy CLI / producer / hooks-download keys keep bypassing).
//   - Enforcement runs even when the org is non-enterprise — plugin
//     scoping is a security primitive, not a tier feature.
func TestAPIKeyPrincipal_PrepareAndEnforce(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	organizationID := "org_authz_apikey_principal"
	apiKeyID := "00000000-0000-0000-0000-0000000000aa"
	toolsetID := "00000000-0000-0000-0000-0000000000bb"
	otherToolsetID := "00000000-0000-0000-0000-0000000000cc"

	seedOrganization(t, t.Context(), conn, organizationID)
	seedGrant(t, t.Context(), conn, organizationID,
		urn.NewPrincipal(urn.PrincipalTypeAPIKey, apiKeyID),
		ScopeMCPConnect, toolsetID,
	)

	apiKeyAuthCtx := func(systemManaged bool, accountType string) *contextvalues.AuthContext {
		return &contextvalues.AuthContext{
			ActiveOrganizationID:  organizationID,
			UserID:                "user_irrelevant_for_apikey",
			ExternalUserID:        "",
			APIKeyID:              apiKeyID,
			APIKeySystemManaged:   systemManaged,
			SessionID:             nil,
			ProjectID:             nil,
			OrganizationSlug:      "",
			Email:                 nil,
			AccountType:           accountType,
			HasActiveSubscription: false,
			Whitelisted:           false,
			ProjectSlug:           nil,
			APIKeyScopes:          []string{"consumer"},
			IsAdmin:               false,
		}
	}

	engine := NewEngine(testinfra.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	t.Run("system-managed key with matching grant: matching toolset succeeds", func(t *testing.T) {
		t.Parallel()
		ctx := contextvalues.SetAuthContext(t.Context(), apiKeyAuthCtx(true, "enterprise"))
		ctx, err := engine.PrepareContext(ctx)
		require.NoError(t, err)

		err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: toolsetID})
		require.NoError(t, err, "scoped api-key must succeed against its bound toolset")
	})

	t.Run("system-managed key with grant: mismatched toolset is forbidden", func(t *testing.T) {
		t.Parallel()
		ctx := contextvalues.SetAuthContext(t.Context(), apiKeyAuthCtx(true, "enterprise"))
		ctx, err := engine.PrepareContext(ctx)
		require.NoError(t, err)

		err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	})

	t.Run("user-managed key (no grants): bypass regardless of check", func(t *testing.T) {
		t.Parallel()
		ctx := contextvalues.SetAuthContext(t.Context(), apiKeyAuthCtx(false, "enterprise"))
		ctx, err := engine.PrepareContext(ctx)
		require.NoError(t, err)

		// Even an obviously-unauthorized check passes — the legacy bypass
		// is what keeps CLI / producer / hooks-download flows working.
		err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
		require.NoError(t, err)
	})

	t.Run("non-enterprise org: api-key grant still enforces", func(t *testing.T) {
		t.Parallel()
		ctx := contextvalues.SetAuthContext(t.Context(), apiKeyAuthCtx(true, "free"))
		ctx, err := engine.PrepareContext(ctx)
		require.NoError(t, err)

		err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr,
			"plugin-scoped key must enforce regardless of account type")
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	})

	t.Run("system-managed key with no grant rows: bypass", func(t *testing.T) {
		t.Parallel()
		// A different api_key id that has system_managed=true but no
		// principal_grants rows (e.g. the org-wide hooks key).
		hooksAuthCtx := apiKeyAuthCtx(true, "enterprise")
		hooksAuthCtx.APIKeyID = "00000000-0000-0000-0000-0000000000dd"
		ctx := contextvalues.SetAuthContext(t.Context(), hooksAuthCtx)
		ctx, err := engine.PrepareContext(ctx)
		require.NoError(t, err)

		err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
		require.NoError(t, err, "system-managed key with zero grants must fall under the bypass policy")
	})
}
