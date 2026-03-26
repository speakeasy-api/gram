package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMiddleware_LoadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), access.ScopeBuildRead, access.WildcardResource)

	endpoint := access.Middleware(logger, ti.conn)(func(ctx context.Context, req any) (any, error) {
		grants, ok := access.GrantsFromContext(ctx)
		require.True(t, ok)
		require.NotNil(t, grants)
		require.NoError(t, access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}))
		return "ok", nil
	})

	res, err := endpoint(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "ok", res)
}

func TestMiddleware_SkipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	endpoint := access.Middleware(logger, ti.conn)(func(ctx context.Context, req any) (any, error) {
		grants, ok := access.GrantsFromContext(ctx)
		require.False(t, ok)
		require.Nil(t, grants)
		return "ok", nil
	})

	res, err := endpoint(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "ok", res)
}

func TestMiddleware_LoadsHardcodedRoleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.UserID = "rbac-test-user"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "admin"), access.ScopeBuildWrite, access.WildcardResource)

	endpoint := access.Middleware(logger, ti.conn)(func(ctx context.Context, req any) (any, error) {
		grants, ok := access.GrantsFromContext(ctx)
		require.True(t, ok)
		require.NotNil(t, grants)
		require.NoError(t, access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}))
		return "ok", nil
	})

	res, err := endpoint(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, "ok", res)
}
