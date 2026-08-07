package authztest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestWithExactGrantsLoadsCallerGrants(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{})
	grants := []authz.Grant{
		{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, "proj_123")},
	}

	ctx = WithExactGrants(t, ctx, grants...)

	loaded, ok := authz.GrantsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, grants, loaded)
}

func TestWithAdminGrants(t *testing.T) {
	t.Parallel()

	ctx := WithAdminGrants(t.Context())
	grants, ok := authz.GrantsFromContext(ctx)
	require.True(t, ok)
	require.Len(t, grants, len(authz.SystemRoleGrants[authz.SystemRoleAdmin]))
	require.True(t, authz.GrantsSatisfy(grants, authz.Check{Scope: authz.ScopeProjectWrite, ResourceID: "project_123"}))
}
