package access

import (
	"testing"

	trequire "github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLoadIntoContext_LoadsUserGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeBuildRead, WildcardResource)

	ctx, err := LoadIntoContext(ctx, logger, ti.conn)
	trequire.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, grants)
	trequire.NoError(t, require(ctx, Check{Scope: ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}))
}

func TestLoadIntoContext_SkipsNonSessionAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	authCtx.SessionID = nil
	authCtx.APIKeyID = "api-key-123"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	ctx, err := LoadIntoContext(ctx, logger, ti.conn)
	trequire.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	trequire.False(t, ok)
	trequire.Nil(t, grants)
}

func TestLoadIntoContext_SkipsNonEnterpriseOrgs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	logger := testenv.NewLogger(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	authCtx.AccountType = "pro"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ScopeBuildRead, WildcardResource)

	ctx, err := LoadIntoContext(ctx, logger, ti.conn)
	trequire.NoError(t, err)

	grants, ok := GrantsFromContext(ctx)
	trequire.False(t, ok)
	trequire.Nil(t, grants)
}
