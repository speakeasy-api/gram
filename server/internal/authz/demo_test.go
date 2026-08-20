package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func demoTestCtx(t *testing.T) context.Context {
	t.Helper()

	ctx := enterpriseTestCtx(t.Context())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ActiveOrganizationID = constants.DemoOrganizationID
	return contextvalues.SetAuthContext(ctx, authCtx)
}

func TestPrepareContext_demoOrgGetsEveryUserVisibleScope(t *testing.T) {
	t.Parallel()

	ctx := demoTestCtx(t)
	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, grants)

	// Demo sessions hold the same set access.ListGrants reports to the
	// dashboard, so no page is offered that the server then refuses.
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeChatRead, ResourceID: "chat_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeEnvironmentRead, ResourceID: "env_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceID: "project_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeMCPWrite, ResourceID: "mcp_demo"}))

	// Internal scopes stay internal: the demo set is the user-visible
	// catalogue, not every scope the server declares.
	for _, grant := range grants {
		visibility, ok := ScopeVisibilityFor(grant.Scope)
		require.True(t, ok, "scope %q has no declared visibility", grant.Scope)
		require.Equal(t, ScopeVisibilityUserVisible, visibility, "scope %q is not user-visible", grant.Scope)
	}
}

// TestDemoScopeGrantsMatchAllScopeGrants pins the demo set to the same
// catalogue impersonating platform admins get. Enforcement drifting narrower
// than what access.ListGrants advertises is the bug this pairing prevents.
func TestDemoScopeGrantsMatchAllScopeGrants(t *testing.T) {
	t.Parallel()

	demo := make([]string, 0, len(DemoScopeGrants()))
	for _, grant := range DemoScopeGrants() {
		demo = append(demo, string(grant.Scope))
	}

	all := make([]string, 0, len(allScopeGrants()))
	for _, grant := range allScopeGrants() {
		all = append(all, string(grant.Scope))
	}

	require.ElementsMatch(t, all, demo)
}

func TestPrepareContext_demoOrgIgnoresScopeOverrides(t *testing.T) {
	t.Parallel()

	ctx := demoTestCtx(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAdminOverrideInContext(ctx, "acme-demo")
	// A scope override must not narrow a demo session either: the demo branch
	// runs before both the override and admin-impersonation branches.
	ctx = contextvalues.SetRBACScopeOverride(ctx, "project:write")

	conn := newTestDB(t)
	engine := NewEngine(testenv.NewLogger(t), conn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	// org:admin is outside the override, so passing it proves the demo branch
	// produced these grants rather than GrantsFromOverrides.
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_demo"}))
}
