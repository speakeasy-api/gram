package authztest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestWithExactGrantsDoesNotMutateCallerSlice(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{})
	grants := []authz.Grant{
		{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, "proj_123")},
	}

	ctx = WithExactGrants(t, ctx, grants...)

	require.Empty(t, grants[0].Effect)

	loaded, ok := authz.GrantsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, authz.PolicyEffectAllow, loaded[0].Effect)
}
