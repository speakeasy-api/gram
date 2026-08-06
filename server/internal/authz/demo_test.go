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

func TestPrepareContext_demoOrgGetsReadOnlyGrants(t *testing.T) {
	t.Parallel()

	ctx := demoTestCtx(t)
	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, grants)

	// Reads allowed.
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_demo"}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeOrgRead, ResourceID: constants.DemoOrganizationID}))
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeChatRead, ResourceID: "chat_demo"}))

	// Writes and environment reads denied.
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceID: "project_demo"}))
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeMCPWrite, ResourceID: "mcp_demo"}))
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeOrgAdmin, ResourceID: constants.DemoOrganizationID}))
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeEnvironmentRead, ResourceID: "env_demo"}))
}

func TestPrepareContext_demoOrgReadOnlyEvenForAdminOverride(t *testing.T) {
	t.Parallel()

	ctx := demoTestCtx(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	ctx = contextvalues.SetAdminOverrideInContext(ctx, "acme-demo")
	// An admin scope override must not widen the demo org back to writes.
	ctx = contextvalues.SetRBACScopeOverride(ctx, "project:write")

	conn := newTestDB(t)
	chConn, err := newClickhouseClient(t)
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), conn, chConn, challengeLoggingAlwaysEnabled, workos.NewStubClient())

	ctx, err = engine.PrepareContext(ctx)
	require.NoError(t, err)

	// The demo branch wins over the admin-override allScopeGrants branch.
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_demo"}))
	require.Error(t, engine.Require(ctx, Check{Scope: ScopeProjectWrite, ResourceID: "project_demo"}))
}
