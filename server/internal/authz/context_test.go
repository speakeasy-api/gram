package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testinfra"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPrepareContext_loadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testinfra.NewLogger(t), conn, RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedConnectedUser(t, ctx, conn, authCtx.ActiveOrganizationID, authCtx.UserID, "test@example.com", "Test User", "user_workos_test", "membership_test")
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.True(t, ok)
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project_123"}))
}

func TestPrepareContext_skipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testinfra.NewLogger(t), conn, RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.False(t, ok)
}

func TestPrepareContext_skipsNonEnterpriseOrgs(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	engine := NewEngine(testinfra.NewLogger(t), conn, RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedOrganization(t, ctx, conn, authCtx.ActiveOrganizationID)
	seedGrant(t, ctx, conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeProjectRead, WildcardResource)

	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	_, ok = GrantsFromContext(ctx)
	require.False(t, ok)
}
