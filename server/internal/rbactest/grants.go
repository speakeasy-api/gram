// Package rbactest provides test helpers for RBAC grant setup in integration tests.
package rbactest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// WithExactAccessGrants marks the context as enterprise and loads the given grants
// directly into the context. Pass no grants to simulate RBAC active with no permissions.
func WithExactAccessGrants(t *testing.T, ctx context.Context, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return authz.GrantsToContext(ctx, grants)
}
