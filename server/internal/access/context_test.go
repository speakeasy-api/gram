package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLoadIntoContext_LoadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), access.ScopeBuildRead, access.WildcardResource)

	ctx, err := access.LoadIntoContext(ctx, logger, ti.conn)
	require.NoError(t, err)

	grants, ok := access.GrantsFromContext(ctx)
	require.True(t, ok)
	require.NotNil(t, grants)
	require.NoError(t, access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}))
}

func TestLoadIntoContext_SkipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx, err := access.LoadIntoContext(ctx, logger, ti.conn)
	require.NoError(t, err)

	grants, ok := access.GrantsFromContext(ctx)
	require.False(t, ok)
	require.Nil(t, grants)
}

func TestLoadIntoContext_SkipsNonEnterpriseOrgs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), access.ScopeBuildRead, access.WildcardResource)

	ctx, err := access.LoadIntoContext(ctx, logger, ti.conn)
	require.NoError(t, err)

	grants, ok := access.GrantsFromContext(ctx)
	require.False(t, ok)
	require.Nil(t, grants)
}
