package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLoadIntoContext_LoadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	manager := NewManager(testLogger(t), ti.conn, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeBuildRead, WildcardResource)

	ctx, err := manager.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.True(t, ok)
	require.NotNil(t, grants)
	require.NoError(t, manager.Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}))
}

func TestLoadIntoContext_SkipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	manager := NewManager(testLogger(t), ti.conn, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx, err := manager.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.False(t, ok)
	require.Nil(t, grants)
}

func TestLoadIntoContext_SkipsNonEnterpriseOrgs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	manager := NewManager(testLogger(t), ti.conn, stubFeatureChecker{enabled: true}, workos.NewStubClient(), cache.NoopCache)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeBuildRead, WildcardResource)

	ctx, err := manager.PrepareContext(ctx)
	require.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	require.False(t, ok)
	require.Nil(t, grants)
}
